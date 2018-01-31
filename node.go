package main

import (
	dropbox "github.com/tj/go-dropbox"
)

type Node struct {
	dropbox.Metadata
	Inode     uint64
	Client    *Dropbox
	NeedsSync bool
}
