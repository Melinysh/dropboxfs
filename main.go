package main

import (
	"log"
	"os"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	dropbox "github.com/tj/go-dropbox"
	"github.com/tj/go-dropy"
)

var inodeCounter uint64 = 0

func NewInode() uint64 {
	inodeCounter += 1
	return inodeCounter
}

func main() {
	if len(os.Args) != 2 {
		log.Println("Must provide mountpoint. Ex: ./dropboxfs ./MyMountPoint")
		return
	}

	token := os.Getenv("DROPBOX_ACCESS_TOKEN")
	if len(token) == 0 {
		log.Panicln("Please provide DROPBOX_ACCESS_TOKEN environment variable")
	}

	mountpoint := os.Args[1]
	log.Println("Will try to mount to mountpoint", mountpoint)
	c, err := fuse.Mount(mountpoint)
	if err != nil {
		log.Fatal("Unable to mount:", err)
	}

	log.Println("Mount successful!")
	defer c.Close()
	<-c.Ready
	// Check if the mount process has an error to report.
	if err := c.MountError; err != nil {
		log.Panicln("Error from mount point:", err)
	}
	if p := c.Protocol(); !p.HasInvalidate() {
		log.Panicln("kernel FUSE support is too old to have invalidations: version %v", p)
	}

	client := dropy.New(dropbox.New(dropbox.NewConfig(token)))
	db := Dropbox{client, &Directory{}}
	rootDir := Directory{
		Node: &Node{Inode: 1, FullPath: "/", NeedsSync: true, Client: &db},
	}
	db.RootDir = &rootDir

	srv := fs.New(c, nil)
	log.Println("Ready to serve FUSE")
	if err := srv.Serve(db); err != nil {
		log.Panicln("Unable to serve filesystem:", err)
	}
	log.Println("Shutting down gracefully...")
}
