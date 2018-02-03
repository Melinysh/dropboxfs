package main

import (
	"bytes"
	"hash/fnv"
	"io/ioutil"
	"log"
	"sync"

	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
)

type Dropbox struct {
	fileClient files.Client
	RootDir    *Directory
	cache      map[string][]*files.Metadata
	fileLookup map[string]*File
	dirLookup  map[string]*Directory
	sync.Mutex
}

func Inode(s string) uint64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return uint64(h.Sum32())
}

func (db Dropbox) Root() (fs.Node, error) {
	db.Lock()
	defer db.Unlock()
	return db.RootDir, nil
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
	}
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
	db.cache[output.Cursor] = metadata

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
		db.cache[output.Cursor] = metadata
	}

	if _, found := db.dirLookup[db.RootDir.Metadata.PathDisplay]; !found {
		db.dirLookup[db.RootDir.Metadata.PathDisplay] = db.RootDir
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
	input.Mode = &files.WriteMode{Tagged: dropbox.Tagged{"overwrite"}}
	output, err := db.fileClient.Upload(input, r)
	if err != nil {
		return nil, err
	}
	file, cached := db.fileLookup[path]
	if cached {
		delete(db.fileLookup, path)
		file.Metadata = output
		db.fileLookup[file.Metadata.PathDisplay] = file
	}
	return output, nil
}

func (db *Dropbox) Move(oldPath string, newPath string) (*files.IsMetadata, error) {
	input := files.NewRelocationArg(oldPath, newPath)
	db.Lock()
	defer db.Unlock()
	output, err := db.fileClient.Move(input)
	if err != nil {
		return nil, err
	}
	delete(db.fileLookup, oldPath)
	delete(db.dirLookup, oldPath)
	return &output, nil
}

func (db *Dropbox) Delete(path string) (*files.IsMetadata, error) {
	db.Lock()
	defer db.Unlock()
	input := files.NewDeleteArg(path)
	output, err := db.fileClient.Delete(input)
	if err != nil {
		return nil, err
	}
	delete(db.fileLookup, path)
	delete(db.dirLookup, path)
	return &output, nil

}

func (db *Dropbox) Mkdir(path string) (*files.FolderMetadata, error) {
	db.Lock()
	defer db.Unlock()
	input := files.NewCreateFolderArg(path)
	metadata, err := db.fileClient.CreateFolder(input)
	if err != nil {
		return nil, err
	}
	db.dirLookup[metadata.PathDisplay] = &Directory{Metadata: metadata}
	return metadata, nil
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
