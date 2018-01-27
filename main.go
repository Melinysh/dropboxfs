package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"bazil.org/fuse/fuseutil"
	dropbox "github.com/tj/go-dropbox"
	"github.com/tj/go-dropy"
	"golang.org/x/net/context"
)

var inodeCounter uint64 = 0

func NewInode() uint64 {
	inodeCounter += 1
	return inodeCounter
}

type Node struct {
	Inode         uint64
	FullPath      string
	LastRefreshed time.Time
}

type Dropbox struct {
	Client  *dropy.Client
	RootDir *Directory
}

func (db Dropbox) Root() (fs.Node, error) {
	return db.RootDir, nil
}

type Directory struct {
	Node
	Subdirectories []os.FileInfo
	Files          []os.FileInfo
}

func (d *Directory) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Println("Requested Attr for Directory", d.FullPath)
	a.Inode = d.Inode
	a.Mode = os.ModeDir | 0700
	return nil
}

func (d *Directory) Lookup(ctx context.Context, name string) (fs.Node, error) {
	log.Println("Requested lookup for ", name)
	db.PopulateDirectory(d)
	for _, n := range d.Files {
		if n.Name() == name {
			log.Println("Found match for directory lookup with size", n.Size())
			return &File{
				Node:     Node{Inode: NewInode(), FullPath: d.FullPath + n.Name(), LastRefreshed: time.Unix(0, 0)},
				Metadata: n,
				Size:     uint64(n.Size()),
			}, nil
		}
	}
	for _, n := range d.Subdirectories {
		if n.Name() == name {
			log.Println("Found match for directory lookup")
			return &Directory{
				Node:           Node{Inode: NewInode(), FullPath: d.FullPath + n.Name() + "/", LastRefreshed: time.Unix(0, 0)},
				Subdirectories: []os.FileInfo{},
				Files:          []os.FileInfo{},
			}, nil
		}
	}
	return nil, fuse.ENOENT
}

func (d *Directory) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Println("Reading all dirs")
	db.PopulateDirectory(d)
	var children []fuse.Dirent
	for _, f := range d.Files {
		children = append(children, fuse.Dirent{Inode: NewInode(), Type: fuse.DT_File, Name: f.Name()})
	}
	for _, dir := range d.Subdirectories {
		children = append(children, fuse.Dirent{Inode: NewInode(), Type: fuse.DT_Dir, Name: dir.Name()})
	}
	log.Println(len(children), " children for dir", d.FullPath)
	return children, nil
}

func (d *Directory) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	log.Println("Create request for name", req.Name)
	f := File{
		Node:     Node{Inode: NewInode(), FullPath: d.FullPath + req.Name, LastRefreshed: time.Now()},
		Metadata: nil,
		Size:     0,
	}
	r := bytes.NewReader([]byte{}) // empty
	if err := db.Client.Upload(f.FullPath, r); err != nil {
		log.Panicln("Unable to create file", f.FullPath, err)
	}
	d.LastRefreshed = time.Unix(0, 0)
	return &f, &f, nil
}

func (d *Directory) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	log.Println("Remove request for ", req.Name)
	if req.Dir {
		newDirs := []os.FileInfo{}
		for _, dir := range d.Subdirectories {
			if dir.Name() != req.Name {
				newDirs = append(newDirs, dir)
			}
		}
		d.Subdirectories = newDirs
		if err := db.Client.Delete(d.FullPath + req.Name); err != nil {
			log.Panicln("Unable to delete item at path", d.FullPath+req.Name, err)
		}

		return nil
	} else { // Remove file
		newFiles := []os.FileInfo{}
		for _, f := range d.Files {
			if f.Name() != req.Name {
				newFiles = append(newFiles, f)
			}
		}
		d.Files = newFiles
		if err := db.Client.Delete(d.FullPath + req.Name); err != nil {
			log.Panicln("Unable to delete item at path", d.FullPath+req.Name, err)
		}

		return nil
	}
	d.LastRefreshed = time.Now()
	return fuse.ENOENT
}

func (d *Directory) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	log.Println("Mkdir request for name", req.Name)
	if err := db.Client.Mkdir(d.FullPath + req.Name); err != nil {
		log.Panicln("Unable to create dir at path", d.FullPath+req.Name, err)
	}
	d.LastRefreshed = time.Unix(0, 0)
	return nil, nil
}

type File struct {
	Node
	Metadata os.FileInfo
	Data     []byte
	Size     uint64
}

func (f *File) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Println("Requested Attr for File", f.FullPath)
	a.Inode = f.Inode
	a.Mode = 0700
	a.Size = f.Size
	return nil
}

func (f *File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	log.Println("Requested Read on File", f.FullPath)
	db.PopulateFile(f)
	fuseutil.HandleRead(req, resp, f.Data)
	return nil
}

func (f *File) ReadAll(ctx context.Context) ([]byte, error) {
	log.Println("Reading all of file", f.FullPath)
	db.PopulateFile(f)
	return f.Data, nil
}

func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	log.Println("Trying to write to ", f.FullPath, "offset", req.Offset, "dataSize:", len(req.Data), "data: ", string(req.Data))
	resp.Size = len(req.Data)
	r := bytes.NewReader(req.Data)
	if err := db.Client.Upload(f.FullPath, r); err != nil {
		log.Panicln("Unable to upload file", f.FullPath, err)
	}
	f.Data = req.Data
	f.Size = uint64(len(req.Data))
	f.LastRefreshed = time.Now()
	log.Println("Wrote to file", f.FullPath)
	req.Respond(resp)
	return nil
}
func (f *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	log.Println("Flushing file", f.FullPath)
	return nil
}
func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	log.Println("Open call on file", f.FullPath)
	return f, nil
}

func (f *File) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	log.Println("Release requested on file", f.FullPath)
	return nil
}

func (f *File) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	log.Println("Fsync call on file", f.FullPath)
	return nil
}

var db Dropbox

func main() {
	log.Println(len(os.Args))
	if len(os.Args) != 2 {
		log.Println("Must provide mountpoint. Ex: ./dropboxfs ./MyMountPoint")
		return
	}
	mountpoint := os.Args[1]
	log.Println("Will try to mount to mountpoint", mountpoint)
	c, err := fuse.Mount(mountpoint)
	if err != nil {
		log.Fatal("Unable to mount:", err)
	}
	log.Println("Mount successful!")

	defer c.Close()
	if p := c.Protocol(); !p.HasInvalidate() {
		log.Panicln("kernel FUSE support is too old to have invalidations: version %v", p)
	}

	token := os.Getenv("DROPBOX_ACCESS_TOKEN")
	if len(token) == 0 {
		log.Panicln("Please provide DROPBOX_ACCESS_TOKEN environment variable")
	}
	client := dropy.New(dropbox.New(dropbox.NewConfig(token)))
	db = Dropbox{client, &Directory{}}
	rootDir := Directory{
		Node:           Node{Inode: 1, FullPath: "/", LastRefreshed: time.Unix(0, 0)},
		Files:          []os.FileInfo{},
		Subdirectories: []os.FileInfo{},
	}
	db.RootDir = &rootDir

	srv := fs.New(c, nil)
	log.Println("Ready to serve FUSE")
	if err := srv.Serve(db); err != nil {
		log.Panicln("Unable to serve filesystem:", err)
	}
	// Check if the mount process has an error to report.
	<-c.Ready
	if err := c.MountError; err != nil {
		log.Panicln("Error from mount point:", err)
	}
}

func (db Dropbox) PopulateDirectory(d *Directory) {
	if time.Since(d.LastRefreshed) < 5*time.Minute {
		log.Println("Directory", d.FullPath, "cached until 5 minutes has passed")
		return
	}
	files, err := db.Client.ListFiles(d.FullPath)
	if err != nil {
		log.Panicln("Unable to load directories at path", d.FullPath)
	}
	folders, err := db.Client.ListFolders(d.FullPath)
	if err != nil {
		log.Panicln("Unable to load files at path", d.FullPath)
	}
	d.Files = files
	d.Subdirectories = folders
	d.LastRefreshed = time.Now()
	log.Println("Populated directory at path", d.FullPath)
}

func (db Dropbox) PopulateFile(f *File) {
	if time.Since(f.LastRefreshed) < 5*time.Minute {
		log.Println("File", f.FullPath, "cached until 5 minutes has passed")
	}
	contents, err := db.Client.Download(f.FullPath)
	defer contents.Close()
	if err != nil {
		log.Panicln("Unable to download file", f.FullPath, err)
	}
	data, err := ioutil.ReadAll(contents)
	f.Data = data
	f.Size = uint64(len(data))
	f.LastRefreshed = time.Now()
}
