package main

import (
	"log"
	"os"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
)

var db Dropbox

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

	config := dropbox.Config{
		Token: token,
	}
	client := files.New(config)
	rootDir := &Directory{
		Metadata: &files.FolderMetadata{},
	}
	db = Dropbox{
		fileClient: client,
		RootDir:    rootDir,
		cache:      map[string][]*files.Metadata{},
		fileLookup: map[string]*File{},
		dirLookup:  map[string]*Directory{},
	}
	srv := fs.New(c, nil)
	log.Println("Ready to serve FUSE")
	if err := srv.Serve(db); err != nil {
		log.Panicln("Unable to serve filesystem:", err)
	}
	log.Println("Shutting down gracefully...")
}
