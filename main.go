package main

import (
	"bufio"
	"bytes"
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	_ "expvar"
	_ "net/http/pprof"

	bazil "bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
	"github.com/melinysh/dropboxfs/fuse"

	log "github.com/sirupsen/logrus"
)

func main() {

	verbosePtr := flag.Bool("v", false, "Enable verbose output")
	mountpointPtr := flag.String("m", "", "Path to FUSE mountpoint")
	tokenFilePtr := flag.String("t", "", "Path to file that contains Dropbox access token")
	stats := flag.Bool("e", false, "Expvar stats on 8080")

	flag.Parse()

	if *stats {
		runtime.SetMutexProfileFraction(5)
		log.Infoln("Starting expvar server at http://localhost:8080")
		go http.ListenAndServe(":8080", nil)
	}
	logLevel := dropbox.LogOff
	if *verbosePtr {
		// Enable for verbose dropbox logging.
		//logLevel = dropbox.LogDebug
		log.SetLevel(log.DebugLevel)
	}

	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// demand mountpoint
	if *mountpointPtr == "" {
		log.Infoln("You must provide a mountpoint with -m")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// if no token file provided, ask for one and write it to disk
	if *tokenFilePtr == "" {
		reader := bufio.NewReader(os.Stdin)
		log.Print("Enter Dropbox access token: ")
		t, err := reader.ReadString('\n')
		if err != nil {
			log.Infoln("Unable to read input", err)
			os.Exit(1)
		}
		token := strings.TrimSpace(t)
		*tokenFilePtr = "./dropbox_token"
		if err = ioutil.WriteFile(*tokenFilePtr, []byte(token), 0600); err != nil {
			log.Infoln("Unable to write dropbox token into", *tokenFilePtr, err)
			os.Exit(1)
		}
		log.Printf("Saved your token to %v\ndropboxfs can use this file later by providing the flag `-t %v`\n", *tokenFilePtr, *tokenFilePtr)
	}

	tokenData, err := ioutil.ReadFile(*tokenFilePtr)
	if err != nil {
		log.Fatalln("Unable to open token file", *tokenFilePtr, err)
	}

	// Files properly end in \n, trim this off to avoid auth issues.
	token := string(bytes.TrimSpace(tokenData))

	log.Infoln("Will try to mount to mountpoint", *mountpointPtr)
	// Always try to unmount in case there was dirty exit
	bazil.Unmount(*mountpointPtr)
	c, err := bazil.Mount(*mountpointPtr)
	if err != nil {
		log.Fatalln("Unable to mount:", err)
	}
	log.Infoln("Mount successful!")
	defer c.Close()
	<-c.Ready
	// Check if the mount process has an error to report.
	if err := c.MountError; err != nil {
		log.Fatalln("Error from mount point:", err)
	}
	if p := c.Protocol(); !p.HasInvalidate() {
		log.Fatalf("kernel FUSE support is too old to have invalidations: version %v\n", p)
	}
	cleanup := func() {
		bazil.Unmount(*mountpointPtr)
	}

	cSignals := make(chan os.Signal)
	signal.Notify(cSignals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-cSignals
		cleanup()
		os.Exit(1)
	}()

	defer cleanup()

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
	log.Infoln("Ready to serve FUSE")
	err = srv.Serve(db)
	if err != nil {
		log.Fatalln("Unable to serve filesystem:", err)
	}
	log.Infoln("Shutting down gracefully...")
}
