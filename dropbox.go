package main

import (
	"bytes"
	"io/ioutil"

	"bazil.org/fuse/fs"
	"github.com/tj/go-dropbox"
)

type Dropbox struct {
	fileClient *dropbox.Files
	RootDir    *Directory
}

func (db Dropbox) Root() (fs.Node, error) {
	return db.RootDir, nil
}

func (db *Dropbox) fetchFolders(path string, files bool) ([]*Node, error) {
	nodes := []*Node{}

	input := dropbox.ListFolderInput{path, false, true, false}
	output, err := db.fileClient.ListFolder(&input)
	if err != nil {
		return nodes, err
	}

	for _, entry := range output.Entries {
		if files && entry.Tag == "folder" {
			continue
		}

		if !files && entry.Tag != "folder" {
			continue
		}

		nodes = append(nodes, &Node{*entry, NewInode(), db, true})
	}

	for output.HasMore {
		nextInput := dropbox.ListFolderContinueInput{output.Cursor}
		output, err = db.fileClient.ListFolderContinue(&nextInput)
		if err != nil {
			return nodes, err
		}
		for _, entry := range output.Entries {
			if files && entry.Tag == "folder" {
				continue
			}

			if !files && entry.Tag != "folder" {
				continue
			}

			nodes = append(nodes, &Node{*entry, NewInode(), db, true})

		}
	}
	// TODO: something with cursors for syncing
	return nodes, nil
}

func (db *Dropbox) ListFiles(path string) ([]*Node, error) {
	return db.fetchFolders(path, true)
}

func (db *Dropbox) ListFolders(path string) ([]*Node, error) {
	return db.fetchFolders(path, false)
}

func (db *Dropbox) Upload(path string, data []byte) (dropbox.Metadata, error) {
	r := bytes.NewReader(data)
	input := dropbox.UploadInput{
		path,
		dropbox.WriteModeOverwrite,
		false,
		false,
		"",
		r,
	}
	output, err := db.fileClient.Upload(&input)
	if err != nil {
		return dropbox.Metadata{}, err
	}
	return output.Metadata, nil
}

func (db *Dropbox) Move(oldPath string, newPath string) (dropbox.Metadata, error) {
	input := dropbox.MoveInput{oldPath, newPath}
	output, err := db.fileClient.Move(&input)
	if err != nil {
		return dropbox.Metadata{}, err
	}
	return output.Metadata, nil

}

func (db *Dropbox) Delete(path string) (dropbox.Metadata, error) {
	input := dropbox.DeleteInput{path}
	output, err := db.fileClient.Delete(&input)
	if err != nil {
		return dropbox.Metadata{}, err
	}
	return output.Metadata, nil

}

func (db *Dropbox) Mkdir(path string) error {
	input := dropbox.CreateFolderInput{path}
	_, err := db.fileClient.CreateFolder(&input)
	return err
}

func (db *Dropbox) Download(path string) ([]byte, error) {
	input := dropbox.DownloadInput{path}
	output, err := db.fileClient.Download(&input)
	if err != nil {
		return []byte{}, err
	}
	defer output.Body.Close()
	return ioutil.ReadAll(output.Body)
}
