package fuse

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
)

type Dropbox struct {
	fileClient files.Client
	rootDir    *Directory
	cache      map[string][]*files.Metadata
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
		return f
	}
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
		return dir
	}
	return &Directory{
		Metadata: metadata,
		Client:   db,
	}
}

// lock assumed
func (db *Dropbox) beginBackgroundPolling(cursor string, metadata []*files.Metadata) {
	db.cache[cursor] = metadata
	go func(c string) {
		for {
			// check if we still need to be polling it
			db.Lock()
			_, found := db.cache[c]
			db.Unlock()
			if !found {
				return
			}
			log.Println("Starting polling call on cursor", c)
			params := map[string]interface{}{"cursor": c, "timeout": 60}
			jsonData, err := json.Marshal(params)
			if err != nil {
				log.Panicln("Unable to create JSON for longpoll", c, params, err)
			}
			resp, err := http.Post("https://notify.dropboxapi.com/2/files/list_folder/longpoll", "application/json", bytes.NewBuffer(jsonData))
			if err != nil {
				log.Panicln("Unable to longpoll on cursor", c, err)
			}

			var output map[string]interface{}
			outputData, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Panicln("Unable to extract json from longpoll response on cursor", c, err)
			}
			resp.Body.Close()
			if err := json.Unmarshal(outputData, &output); err != nil {
				log.Panicln("Unable to extract json map from longpoll response on cursor", c, err)
			}

			// if we detect changes, evict it from cache
			if output["changes"].(bool) {
				log.Println("Change detected for cursor", c, ".Evicting it from cache.")
				db.Lock()
				ms := db.cache[c]
				delete(db.cache, c)
				for _, m := range ms {
					delete(db.fileLookup, m.PathDisplay)
					delete(db.dirLookup, m.PathDisplay)
					log.Println("Removed item at path", m.PathDisplay, "from cache.")
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
					log.Println("Evicted parent directory at path", parentPathDisplay)
				}
				return // don't detect anymore
			} else { // just wait and poll again
				extraSleep, ok := output["backoff"]
				time.Sleep(time.Second * 5)
				if ok {
					log.Println("Dropbox requested backoff for cursor", c, ",", extraSleep, "seconds")
					time.Sleep(time.Second * time.Duration(extraSleep.(int)))
				}
			}
		}
	}(cursor)
}

// lock assumed
func (db *Dropbox) fetchItems(path string) ([]files.IsMetadata, error) {
	nodes := []files.IsMetadata{}
	log.Println("Looking up items for path", path)
	input := files.NewListFolderArg(path)
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
		log.Println("Going for another round of fetching for path", path)
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
