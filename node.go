package main

import (
	"os"
)

type Node struct {
	Inode     uint64
	FullPath  string
	Name      string
	Client    *Dropbox
	Size      uint64
	NeedsSync bool
}

func NewNode(info os.FileInfo, parentDir *Directory) *Node {
	suffix := ""
	if info.IsDir() {
		suffix = "/"
	}
	return &Node{
		Inode:     NewInode(),
		Name:      info.Name(),
		FullPath:  parentDir.FullPath + info.Name() + suffix,
		Client:    parentDir.Client,
		Size:      uint64(info.Size()),
		NeedsSync: true,
	}
}

func NewNodes(infos []os.FileInfo, parentDir *Directory) []*Node {
	nodes := []*Node{}
	for _, i := range infos {
		nodes = append(nodes, NewNode(i, parentDir))
	}
	return nodes
}
