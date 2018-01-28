package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"time"

	"golang.org/x/net/context"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"bazil.org/fuse/fuseutil"
)

type File struct {
	*Node
	Data []byte
}

func (f *File) PopulateFile() {
	if time.Since(f.LastRefreshed) < 5*time.Minute {
		log.Println("File", f.FullPath, "cached until 5 minutes has passed")
	}
	contents, err := f.Client.Download(f.FullPath)
	defer contents.Close()
	if err != nil {
		log.Panicln("Unable to download file", f.FullPath, err)
	}
	data, err := ioutil.ReadAll(contents)
	f.Data = data
	f.Size = uint64(len(data))
	f.LastRefreshed = time.Now()
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
	f.PopulateFile()
	fuseutil.HandleRead(req, resp, f.Data)
	return nil
}

func (f *File) ReadAll(ctx context.Context) ([]byte, error) {
	log.Println("Reading all of file", f.FullPath)
	f.PopulateFile()
	return f.Data, nil
}

func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	log.Println("Trying to write to ", f.FullPath, "offset", req.Offset, "dataSize:", len(req.Data), "data: ", string(req.Data))
	resp.Size = len(req.Data)
	r := bytes.NewReader(req.Data)
	if err := f.Client.Upload(f.FullPath, r); err != nil {
		log.Panicln("Unable to upload file", f.FullPath, err)
	}
	f.Data = req.Data
	f.Size = uint64(len(req.Data))
	f.LastRefreshed = time.Now()
	log.Println("Wrote to file", f.FullPath)
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
