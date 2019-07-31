package fuse

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"io/ioutil"
	"net/http"
	"path"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
	cmap "github.com/orcaman/concurrent-map"
)

type Dropbox struct {
	fileClient files.Client
	rootDir    *Directory
	pathCache  cmap.ConcurrentMap // map[string]string
	fileLookup cmap.ConcurrentMap // map[string]*File
	dirLookup  cmap.ConcurrentMap // map[string]*Directory
	sync.Mutex
}

func NewDropbox(c files.Client, root *Directory) *Dropbox {
	db := &Dropbox{
		fileClient: c,
		rootDir:    root,
		pathCache:  cmap.New(),
		fileLookup: cmap.New(),
		dirLookup:  cmap.New(),
	}
	root.Client = db
	// Start polling for changes
	// According to https://www.dropboxforum.com/t5/API-Support-Feedback/API-v2-Long-polling/td-p/247873
	// And official docs this is account wide despite what folder is passed in.
	go db.getRecursiveCursor("")
	return db
}

func Inode(s string) uint64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return uint64(h.Sum32())
}

func (db Dropbox) Root() (fs.Node, error) {
	return db.rootDir, nil
}

func (db *Dropbox) IsFileCached(f *File) bool {
	_, found := db.fileLookup.Get(f.Metadata.PathDisplay)
	return found
}

func (db *Dropbox) NewOrCachedFile(metadata *files.FileMetadata) *File {
	if t, found := db.fileLookup.Get(metadata.PathDisplay); found {
		f := t.(*File)
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
	_, found := db.dirLookup.Get(d.Metadata.PathDisplay)
	return found
}

func (db *Dropbox) NewOrCachedDirectory(metadata *files.FolderMetadata) *Directory {
	t, found := db.dirLookup.Get(metadata.PathDisplay)
	if found {
		dir := t.(*Directory)
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
func (db *Dropbox) beginBackgroundPolling(cursor, path string) {
	if _, found := db.pathCache.Get(path); found {
		log.Infoln("Polling already running for path ", path)
		return
	}
	delay := func() {
		time.Sleep(250 * time.Millisecond)
	}

	db.pathCache.Set(path, cursor)
	log.Infof("Starting polling call on path: '%s' for cursor: %s", path, cursorSHA(cursor))
	go func(c string) {
		for {
			// check if we still need to be polling it
			log.Infof("Polling call on path: '%s'", path)
			// Setup consumer of the polling
			// Setup the async polling
			output, retry := longpoll(c)
			if retry {
				delay()
				continue
			}

			if output["changes"].(bool) {
				log.Infof("Change detected for path: '%s'\n", path)
				nodes, cursor, err := db.listFolderAll(c)
				log.Debugf("Nodes %+v", nodes)
				if err != nil {
					log.Errorf("Error fetching Dropbox changes %s\n", err)
					continue
				}
				err = db.applyChanges(nodes)
				if err != nil {
					log.Errorln("Unable to apply changes", err)
				}
				// Follow up with the next cursor
				log.Debugf("Switching out old cursor(%s) for new one (%s)", cursorSHA(c), cursorSHA(cursor))
				c = cursor
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

func (db *Dropbox) applyChanges(nodes []files.IsMetadata) error {
	for _, entry := range nodes {
		switch v := entry.(type) {
		case *files.FileMetadata:
			db.fileLookup.Set(v.PathDisplay, &File{
				Metadata: v,
				Client:   db,
			})
			// TODO: Merge into parent d.Files slice
			db.evictParentFolder(v.PathDisplay)
			log.Debugln("Added file at path", v.PathDisplay)
		case *files.FolderMetadata:
			db.dirLookup.Set(v.PathDisplay, &Directory{
				Metadata: v,
				Client:   db,
			})
			// TODO: Merge into parent d.Files slice
			db.evictParentFolder(v.PathDisplay)
			log.Debugln("Added folder at path", v.PathDisplay)
		case *files.DeletedMetadata:
			db.fileLookup.Remove(v.PathDisplay)
			db.dirLookup.Remove(v.PathDisplay)
			db.evictParentFolder(v.PathDisplay)
			log.Debugln("Removed item at path", v.PathDisplay)
		default:
			log.Errorf("Unhandled change: %+v", v)
		}
	}
	return nil
}

func (db *Dropbox) parentFolder(pathDisplay string) string {
	parent := path.Dir(pathDisplay)
	if parent == "." {
		parent = ""
	}
	return parent
}

// TODO: setup background refresh for these folders
func (db *Dropbox) evictParentFolder(pathDisplay string) {
	// TODO: determine correct way to handle this situation
	// Otherwise the parent is missing/has the added/deleted file
	parentPathDisplay := db.parentFolder(pathDisplay)
	db.dirLookup.Remove(parentPathDisplay)
	log.Infoln("Evicted parent directory at path", parentPathDisplay)
}

func (db *Dropbox) getRecursiveCursor(path string) (string, error) {
	input := files.NewListFolderArg(path)
	input.Limit = 2000
	input.Recursive = true
	output, err := db.fileClient.ListFolderGetLatestCursor(input)
	if err != nil {
		return "", err
	}
	db.beginBackgroundPolling(output.Cursor, path)
	return output.Cursor, nil
}

// lock assumed
func (db *Dropbox) fetchItems(path string) ([]files.IsMetadata, error) {
	nodes := []files.IsMetadata{}
	log.Debugln("Looking up items for path", path)
	input := files.NewListFolderArg(path)
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
	}

	if _, found := db.dirLookup.Get(db.rootDir.Metadata.PathDisplay); !found {
		db.dirLookup.Set(db.rootDir.Metadata.PathDisplay, db.rootDir)
	}

	return nodes, nil
}

func (db *Dropbox) listFolderAll(cursor string) ([]files.IsMetadata, string, error) {
	nodes := []files.IsMetadata{}
	arg := files.NewListFolderContinueArg(cursor)
	log.Debugln("listFolderAll: starting")
	output, err := db.fileClient.ListFolderContinue(arg)
	if err != nil {
		log.Errorln("Error with ListFolderContinue", err)
		return nil, cursor, err
	}
	log.Debugf("listFolderAll: starting result %+v\n", output.Entries)
	nodes = append(nodes, output.Entries...)
	for output.HasMore {
		log.Debugln("listFolderAll: fetching more")
		arg := files.NewListFolderContinueArg(output.Cursor)
		output, err = db.fileClient.ListFolderContinue(arg)
		if err != nil {
			return nil, cursor, err
		}
		nodes = append(nodes, output.Entries...)
	}
	return nodes, output.Cursor, nil
}

func (db *Dropbox) listFolderContinueAll(cursor string) ([]*files.Metadata, string, error) {
	nodes, c, err := db.listFolderAll(cursor)
	if err != nil {
		log.Errorf("Error with listFolderAll %s", err)
		return nil, cursor, err
	}

	metadata := []*files.Metadata{}
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
	return metadata, c, nil
}

func (db *Dropbox) listFilesAndFolders(d *Directory) ([]*files.FileMetadata, []*files.FolderMetadata, error) {
	// Can only reliably be called inside ListFiles or ListFolders
	path := d.Metadata.PathDisplay
	out, err := db.fetchItems(path)
	filesMetadata := []*files.FileMetadata{}
	folderMetadata := []*files.FolderMetadata{}
	for _, metadata := range out {
		switch v := metadata.(type) {
		case *files.FileMetadata:
			filesMetadata = append(filesMetadata, v)
		case *files.FolderMetadata:
			folderMetadata = append(folderMetadata, v)
		}
	}
	db.dirLookup.Set(path, d)
	return filesMetadata, folderMetadata, err
}

func (db *Dropbox) ListFiles(d *Directory) ([]*files.FileMetadata, error) {
	fx, _, err := db.listFilesAndFolders(d)
	return fx, err
}

func (db *Dropbox) ListFolders(d *Directory) ([]*files.FolderMetadata, error) {
	_, folders, err := db.listFilesAndFolders(d)
	return folders, err
}

func (db *Dropbox) Upload(path string, data []byte) (*files.FileMetadata, error) {
	r := bytes.NewReader(data)
	input := files.NewCommitInfo(path)
	input.Mute = true // don't send user notification on other clients
	input.Mode = &files.WriteMode{Tagged: dropbox.Tagged{"overwrite"}}
	output, err := db.fileClient.Upload(input, r)
	if err != nil {
		return nil, err
	}
	fileT, cached := db.fileLookup.Get(path)

	// if cached update to latest path
	if cached {
		db.Lock()
		db.fileLookup.Remove(path)
		file := fileT.(*File)
		file.Metadata = output
		db.fileLookup.Set(file.Metadata.PathDisplay, file)
		db.Unlock()
	}
	return output, nil
}

func (db *Dropbox) Move(oldPath string, newPath string) (files.IsMetadata, error) {
	input := files.NewRelocationArg(oldPath, newPath)
	output, err := db.fileClient.MoveV2(input)
	if err != nil {
		return nil, err
	}
	db.fileLookup.Remove(oldPath)
	db.dirLookup.Remove(oldPath)
	return output.Metadata, nil
}

func (db *Dropbox) Delete(path string) (files.IsMetadata, error) {
	input := files.NewDeleteArg(path)
	output, err := db.fileClient.DeleteV2(input)
	if err != nil {
		return nil, err
	}
	db.fileLookup.Remove(path)
	db.dirLookup.Remove(path)
	return output.Metadata, nil

}

func (db *Dropbox) Mkdir(path string) (*files.FolderMetadata, error) {
	input := files.NewCreateFolderArg(path)
	output, err := db.fileClient.CreateFolderV2(input)
	if err != nil {
		return nil, err
	}
	db.dirLookup.Set(output.Metadata.PathDisplay, &Directory{Metadata: output.Metadata, Client: db})
	return output.Metadata, nil
}

func (db *Dropbox) Download(path string) ([]byte, error) {
	input := files.NewDownloadArg(path)
	_, content, err := db.fileClient.Download(input)
	if err != nil {
		return []byte{}, err
	}
	defer content.Close()
	return ioutil.ReadAll(content)
}

// Credit: https://gist.github.com/unakatsuo/0dcab7898d092d87a77d684f3e71621b
// Cursor api calls do not use auth headers because it's baked into the cursor itself.
type noauthTransport struct {
	http.Transport
}

func (t *noauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Del("Authorization")
	return t.Transport.RoundTrip(req)
}

func newNoAuthClient() *http.Client {
	return &http.Client{
		Transport: &noauthTransport{},
	}
}

// End credit

// TODO: switch this fn in for existing folder polling method
// this uses the existing dropbox sdk with noAuth shim instead of needing
// to roll our own with http client.
func (db *Dropbox) FolderPoll(cursor string, c chan []files.IsMetadata) error {
	for {
		noauthdbx := files.New(dropbox.Config{Client: newNoAuthClient()})
		req := files.NewListFolderLongpollArg(cursor)
		res, err := noauthdbx.ListFolderLongpoll(req)
		if err != nil {
			return err
		}
		if res.Backoff > 0 {
			time.Sleep(time.Duration(res.Backoff) * time.Second)
		}
		// Continue using the same cursor to check for changes
		if !res.Changes {
			continue
		}
		log.Infoln("There is a change")
		req2 := files.NewListFolderContinueArg(cursor)
		res2, err := db.fileClient.ListFolderContinue(req2)
		if err != nil {
			return err
		}
		c <- res2.Entries
		for res2.HasMore {
			req2 = files.NewListFolderContinueArg(res2.Cursor)
			res2, err = db.fileClient.ListFolderContinue(req2)
			if err != nil {
				return err
			}
			c <- res2.Entries
		}
		cursor = res2.Cursor
	}
}
