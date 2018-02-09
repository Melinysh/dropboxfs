package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	bazil "bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
	"github.com/melinysh/dropboxfs/fuse"
)

func main() {

	verbosePtr := flag.Bool("v", false, "Enable verbose output")
	mountpointPtr := flag.String("m", "", "Path to FUSE mountpoint")
	tokenFilePtr := flag.String("t", "", "Path to file that contains Dropbox access token")

	flag.Parse()

	// demand mountpoint
	if *mountpointPtr == "" {
		fmt.Println("You must provide a mountpoint with -m")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// if no token file provided, ask for one and write it to disk
	if *tokenFilePtr == "" {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Enter Dropbox access token: ")
		token, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Unable to read input", err)
			os.Exit(1)
		}
		*tokenFilePtr = "./dropbox_token"
		if err = ioutil.WriteFile(*tokenFilePtr, []byte(token[:len(token)-1]), 0600); err != nil {
			fmt.Println("Unable to write dropbox token into", *tokenFilePtr, err)
			os.Exit(1)
		}
		fmt.Printf("Saved your token to %v\ndropboxfs can use this file later by providing the flag `-t %v`\n", *tokenFilePtr, *tokenFilePtr)
	}

	tokenData, err := ioutil.ReadFile(*tokenFilePtr)
	if err != nil {
		log.Println("Unable to open token file", *tokenFilePtr, err)
	}
	token := string(tokenData)

	log.Println("Will try to mount to mountpoint", *mountpointPtr)
	c, err := bazil.Mount(*mountpointPtr)
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

	logLevel := dropbox.LogOff
	if *verbosePtr {
		logLevel = dropbox.LogDebug
	}
	config := dropbox.Config{
		Token:    token,
		LogLevel: logLevel,
	}
	client := files.New(config)
	rootDir := &fuse.Directory{
		Metadata: &files.FolderMetadata{},
	}
	db := fuse.NewDropbox(client, rootDir)

	srv := fs.New(c, nil)
	log.Println("Ready to serve FUSE")
	if err := srv.Serve(db); err != nil {
		log.Panicln("Unable to serve filesystem:", err)
	}
	log.Println("Shutting down gracefully...")
}
