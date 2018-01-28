package main

import (
	"bazil.org/fuse/fs"
	"github.com/tj/go-dropy"
)

type Dropbox struct {
	*dropy.Client
	RootDir *Directory
}

func (db Dropbox) Root() (fs.Node, error) {
	return db.RootDir, nil
}
