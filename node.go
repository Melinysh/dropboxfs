package main

import (
	"os"
	"time"
)

type Node struct {
	Inode         uint64
	FullPath      string
	Name          string
	LastRefreshed time.Time
	Client        *Dropbox
	Size          uint64
}

func NewNode(info os.FileInfo, parentDir *Directory) *Node {
	suffix := ""
	if info.IsDir() {
		suffix = "/"
	}
	return &Node{
		Inode:         NewInode(),
		Name:          info.Name(),
		FullPath:      parentDir.FullPath + info.Name() + suffix,
		Client:        parentDir.Client,
		Size:          uint64(info.Size()),
		LastRefreshed: time.Unix(0, 0),
	}
}

func NewNodes(infos []os.FileInfo, parentDir *Directory) []*Node {
	nodes := []*Node{}
	for _, i := range infos {
		nodes = append(nodes, NewNode(i, parentDir))
	}
	return nodes
}
