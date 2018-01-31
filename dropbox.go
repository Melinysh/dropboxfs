package main

import (
	"bytes"
	"hash/fnv"
	"io/ioutil"
	"log"

	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
)

type Dropbox struct {
	fileClient files.Client
	RootDir    *Directory
}

func Inode(s string) uint64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return uint64(h.Sum32())
}

func (db Dropbox) Root() (fs.Node, error) {
	return db.RootDir, nil
}

func (db *Dropbox) fetchItems(path string) ([]files.IsMetadata, error) {
	nodes := []files.IsMetadata{}
	log.Println("Looking up items for path", path)
	input := files.NewListFolderArg(path)
	output, err := db.fileClient.ListFolder(input)
	if err != nil {
		return nodes, err
	}

	for _, entry := range output.Entries {
		nodes = append(nodes, entry)
	}

	for output.HasMore {
		log.Println("Going for another round of fetching for path", path)
		nextInput := files.NewListFolderContinueArg(output.Cursor)
		output, err = db.fileClient.ListFolderContinue(nextInput)
		if err != nil {
			return nodes, err
		}
		for _, entry := range output.Entries {
			nodes = append(nodes, entry)
		}
	}
	// TODO: something with cursors for syncing
	return nodes, nil
}

func (db *Dropbox) ListFiles(path string) ([]*files.FileMetadata, error) {
	out, err := db.fetchItems(path)
	filesMetadata := []*files.FileMetadata{}
	for _, metadata := range out {
		m, ok := (metadata).(*files.FileMetadata)
		if ok {
			log.Println("Adding file", m.Name)
			filesMetadata = append(filesMetadata, m)
		} else {
			log.Println("Skipping folder")
		}
	}
	return filesMetadata, err

}

func (db *Dropbox) ListFolders(path string) ([]*files.FolderMetadata, error) {
	out, err := db.fetchItems(path)
	folderMetadata := []*files.FolderMetadata{}
	for _, metadata := range out {
		m, ok := (metadata).(*files.FolderMetadata)
		if ok {
			log.Println("Adding folder", m.Name)
			folderMetadata = append(folderMetadata, m)
		} else {

			log.Println("Skipping file")
		}
	}
	return folderMetadata, err
}

func (db *Dropbox) Upload(path string, data []byte) (*files.FileMetadata, error) {
	r := bytes.NewReader(data)
	input := files.NewCommitInfo(path)
	input.Mode = &files.WriteMode{Tagged: dropbox.Tagged{"overwrite"}}
	return db.fileClient.Upload(input, r)
}

func (db *Dropbox) Move(oldPath string, newPath string) (*files.IsMetadata, error) {
	input := files.NewRelocationArg(oldPath, newPath)
	output, err := db.fileClient.Move(input)
	if err != nil {
		return nil, err
	}
	return &output, nil
}

func (db *Dropbox) Delete(path string) (*files.IsMetadata, error) {
	input := files.NewDeleteArg(path)
	output, err := db.fileClient.Delete(input)
	if err != nil {
		return nil, err
	}
	return &output, nil

}

func (db *Dropbox) Mkdir(path string) (*files.FolderMetadata, error) {
	input := files.NewCreateFolderArg(path)
	return db.fileClient.CreateFolder(input)
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
