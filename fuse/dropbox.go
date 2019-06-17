package fuse

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
)

type Dropbox struct {
	fileClient files.Client
	rootDir    *Directory
	cache      map[string][]*files.Metadata
	pathCache  map[string]string
	fileLookup map[string]*File
	dirLookup  map[string]*Directory
	sync.Mutex
}

func NewDropbox(c files.Client, root *Directory) *Dropbox {
	db := &Dropbox{
		fileClient: c,
		rootDir:    root,
		cache:      map[string][]*files.Metadata{},
		fileLookup: map[string]*File{},
		dirLookup:  map[string]*Directory{},
		pathCache:  make(map[string]string),
	}
	root.Client = db
	return db
}

func Inode(s string) uint64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return uint64(h.Sum32())
}

func (db Dropbox) Root() (fs.Node, error) {
	db.Lock()
	defer db.Unlock()
	return db.rootDir, nil
}

func (db *Dropbox) IsFileCached(f *File) bool {
	db.Lock()
	defer db.Unlock()
	_, found := db.fileLookup[f.Metadata.PathDisplay]
	return found
}

func (db *Dropbox) NewOrCachedFile(metadata *files.FileMetadata) *File {
	db.Lock()
	defer db.Unlock()
	f, found := db.fileLookup[metadata.PathDisplay]
	if found {
		log.Debugln("Returning cached file", metadata.PathDisplay)
		return f
	}
	log.Debugln("Returning uncached file", metadata.PathDisplay)
	return &File{
		Metadata: metadata,
		Client:   db,
	}
}

func (db *Dropbox) IsDirectoryCached(d *Directory) bool {
	db.Lock()
	defer db.Unlock()
	_, found := db.dirLookup[d.Metadata.PathDisplay]
	return found
}

func (db *Dropbox) NewOrCachedDirectory(metadata *files.FolderMetadata) *Directory {
	db.Lock()
	defer db.Unlock()
	dir, found := db.dirLookup[metadata.PathDisplay]
	if found {
		log.Debugln("Returning cached dir", metadata.PathDisplay)
		return dir
	}
	log.Debugln("Returning uncached dir", metadata.PathDisplay)
	return &Directory{
		Metadata: metadata,
		Client:   db,
	}
}

func cursorSHA(s string) string {
	h := sha1.New()
	h.Write([]byte(s))
	sha1_hash := hex.EncodeToString(h.Sum(nil))
	return sha1_hash
}

func longpoll(c string) (map[string]interface{}, bool) {
	params := map[string]interface{}{"cursor": c, "timeout": 60}
	jsonData, err := json.Marshal(params)
	if err != nil {
		log.Errorln("Unable to create JSON for longpoll", c, params, err)
		return nil, true
	}
	// Long poll becomes very inefficient for large file counts
	resp, err := http.Post("https://notify.dropboxapi.com/2/files/list_folder/longpoll", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		// TODO: retry
		log.Errorln("Unable to longpoll on cursor", c, err)
		return nil, true
	}

	var output map[string]interface{}
	outputData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		// TODO: retry
		log.Errorln("Unable to extract json from longpoll response on cursor", c, err)
		return nil, true
	}
	resp.Body.Close()

	if err := json.Unmarshal(outputData, &output); err != nil {
		log.Errorln("Unable to extract json map from longpoll response on cursor", c, err)
		return nil, true
	}
	return output, false
}

// Long polling reimplemented due to Dropbox Go SDK having broken implementation
// Source: https://github.com/dropbox/dropbox-sdk-go-unofficial/issues/7
// Lock assumed
func (db *Dropbox) beginBackgroundPolling(cursor string, metadata []*files.Metadata) {
	db.cache[cursor] = metadata
	log.Infof("Starting polling call on path: %s for cursor: %s", metadata[0].PathDisplay, cursorSHA(cursor))
	go func(c string) {
		for {
			// check if we still need to be polling it
			db.Lock()
			m, found := db.cache[c]
			db.Unlock()
			if !found {
				return
			}
			delay := func() {
				time.Sleep(250 * time.Millisecond)
			}

			log.Infof("Polling call on path: %s", m[0].PathDisplay)
			output, retry := longpoll(c)
			if retry {
				delay()
				continue
			}

			// TODO:
			// - consider using list_folder/continue to get the specific changes vs evicting all the child nodes.
			// https://www.dropbox.com/developers/documentation/http/documentation#files-list_folder-continue
			// - Dedupe the calls in order to not longpoll on every file/folder, but instead
			// do strategic ones.
			// if we detect changes, evict it from cache
			// Set long polling at top level of Dropbox? And then iterate over changes via continue
			// vs starting many many duplicated cursors?
			if output["changes"].(bool) {
				log.Infoln("Change detected for path: %s", m[0].PathDisplay, ".Evicting it from cache.")
				db.Lock()
				ms := db.cache[c]
				delete(db.cache, c)
				for _, m := range ms {
					delete(db.fileLookup, m.PathDisplay)
					delete(db.dirLookup, m.PathDisplay)
					log.Infoln("Removed item at path", m.PathDisplay, "from cache.")
				}
				db.Unlock()
				// also evict parent directory from cache
				if len(ms) > 0 {
					lastSlash := strings.LastIndex(ms[0].PathDisplay, "/")
					parentPathDisplay := "" // default to root dir
					if lastSlash > 0 {
						parentPathDisplay = ms[0].PathDisplay[:lastSlash]
					}
					db.Lock()
					delete(db.dirLookup, parentPathDisplay)
					db.Unlock()
					log.Infoln("Evicted parent directory at path", parentPathDisplay)
				}
				return // don't detect anymore
			} else { // just wait and poll again
				extraSleep, ok := output["backoff"]
				time.Sleep(time.Second * 5)
				if ok {
					log.Warnln("Dropbox requested backoff for cursor", c, ",", extraSleep, "seconds")
					time.Sleep(time.Second * time.Duration(extraSleep.(int)))
				}
			}
		}
	}(cursor)
}

// lock assumed
func (db *Dropbox) fetchItems(path string) ([]files.IsMetadata, error) {
	nodes := []files.IsMetadata{}
	log.Debugln("Looking up items for path", path)
	input := files.NewListFolderArg(path)
	// Debug how to get recursive working vs blocking
	//input.Recursive = true
	input.Limit = 2000
	output, err := db.fileClient.ListFolder(input)
	if err != nil {
		return nodes, err
	}

	metadata := []*files.Metadata{}
	for _, entry := range output.Entries {
		nodes = append(nodes, entry)
		if fileMetadata, isFile := entry.(*files.FileMetadata); isFile {
			metadata = append(metadata, &fileMetadata.Metadata)
		} else {
			folderMetadata := entry.(*files.FolderMetadata)
			metadata = append(metadata, &folderMetadata.Metadata)
		}
	}
	db.beginBackgroundPolling(output.Cursor, metadata)

	for output.HasMore {
		log.Infoln("Going for another round of fetching for path", path)
		metadata := []*files.Metadata{}
		nextInput := files.NewListFolderContinueArg(output.Cursor)
		output, err = db.fileClient.ListFolderContinue(nextInput)
		if err != nil {
			return nodes, err
		}
		for _, entry := range output.Entries {
			nodes = append(nodes, entry)
			if fileMetadata, isFile := entry.(*files.FileMetadata); isFile {
				metadata = append(metadata, &fileMetadata.Metadata)
			} else {
				folderMetadata := entry.(*files.FolderMetadata)
				metadata = append(metadata, &folderMetadata.Metadata)
			}
		}
		db.beginBackgroundPolling(output.Cursor, metadata)
	}

	if _, found := db.dirLookup[db.rootDir.Metadata.PathDisplay]; !found {
		db.dirLookup[db.rootDir.Metadata.PathDisplay] = db.rootDir
	}

	return nodes, nil
}

func (db *Dropbox) listFolderContinueAll(cursor string) ([]*files.Metadata, error) {
	nodes := []files.IsMetadata{}
	metadata := []*files.Metadata{}
	nextInput := files.NewListFolderContinueArg(cursor)
	output, err := db.fileClient.ListFolderContinue(nextInput)
	nodes = append(nodes, output.Entries...)
	for output.HasMore {
		nextInput := files.NewListFolderContinueArg(output.Cursor)
		output, err = db.fileClient.ListFolderContinue(nextInput)
		if err != nil {
			return metadata, err
		}
		nodes = append(nodes, output.Entries...)
	}

	for _, entry := range nodes {
		switch v := entry.(type) {
		case *files.FileMetadata:
			metadata = append(metadata, &v.Metadata)
		case *files.FolderMetadata:
			metadata = append(metadata, &v.Metadata)
		case *files.DeletedMetadata:
			metadata = append(metadata, &v.Metadata)
		}
	}
	return metadata, nil
}

func (db *Dropbox) ListFiles(d *Directory) ([]*files.FileMetadata, error) {
	path := d.Metadata.PathDisplay
	db.Lock()
	defer db.Unlock()
	out, err := db.fetchItems(path)
	filesMetadata := []*files.FileMetadata{}
	for _, metadata := range out {
		m, ok := (metadata).(*files.FileMetadata)
		if ok {
			filesMetadata = append(filesMetadata, m)
		}
	}
	db.dirLookup[path] = d
	return filesMetadata, err
}

func (db *Dropbox) ListFolders(d *Directory) ([]*files.FolderMetadata, error) {
	path := d.Metadata.PathDisplay
	db.Lock()
	defer db.Unlock()
	out, err := db.fetchItems(path)
	folderMetadata := []*files.FolderMetadata{}
	for _, metadata := range out {
		m, ok := (metadata).(*files.FolderMetadata)
		if ok {
			folderMetadata = append(folderMetadata, m)
		}
	}
	db.dirLookup[path] = d
	return folderMetadata, err
}

func (db *Dropbox) Upload(path string, data []byte) (*files.FileMetadata, error) {
	db.Lock()
	defer db.Unlock()
	r := bytes.NewReader(data)
	input := files.NewCommitInfo(path)
	input.Mute = true // don't send user notification on other clients
	input.Mode = &files.WriteMode{Tagged: dropbox.Tagged{"overwrite"}}
	output, err := db.fileClient.Upload(input, r)
	if err != nil {
		return nil, err
	}
	file, cached := db.fileLookup[path]

	// if cached update to latest path
	if cached {
		delete(db.fileLookup, path)
		file.Metadata = output
		db.fileLookup[file.Metadata.PathDisplay] = file
	}
	return output, nil
}

func (db *Dropbox) Move(oldPath string, newPath string) (files.IsMetadata, error) {
	input := files.NewRelocationArg(oldPath, newPath)
	db.Lock()
	defer db.Unlock()
	output, err := db.fileClient.MoveV2(input)
	if err != nil {
		return nil, err
	}
	delete(db.fileLookup, oldPath)
	delete(db.dirLookup, oldPath)
	return output.Metadata, nil
}

func (db *Dropbox) Delete(path string) (files.IsMetadata, error) {
	db.Lock()
	defer db.Unlock()
	input := files.NewDeleteArg(path)
	output, err := db.fileClient.DeleteV2(input)
	if err != nil {
		return nil, err
	}
	delete(db.fileLookup, path)
	delete(db.dirLookup, path)
	return output.Metadata, nil

}

func (db *Dropbox) Mkdir(path string) (*files.FolderMetadata, error) {
	db.Lock()
	defer db.Unlock()
	input := files.NewCreateFolderArg(path)
	output, err := db.fileClient.CreateFolderV2(input)
	if err != nil {
		return nil, err
	}
	db.dirLookup[output.Metadata.PathDisplay] = &Directory{Metadata: output.Metadata, Client: db}
	return output.Metadata, nil
}

func (db *Dropbox) Download(path string) ([]byte, error) {
	db.Lock()
	defer db.Unlock()
	input := files.NewDownloadArg(path)
	_, content, err := db.fileClient.Download(input)
	if err != nil {
		return []byte{}, err
	}
	defer content.Close()
	return ioutil.ReadAll(content)
}
