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
	Data        []byte
	NeedsUpload bool
}

func (f *File) PopulateFile() {
	if time.Since(f.LastRefreshed) < 5*time.Minute {
		log.Println("File", f.FullPath, "cached until 5 minutes has passed")
		return
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
	f.NeedsUpload = false
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
	log.Println("Trying to write to ", f.FullPath, "offset", req.Offset, "dataSize:", len(req.Data))
	resp.Size = len(req.Data)
	newData := f.Data[:req.Offset]
	newData = append(newData, req.Data...)
	if secondOffset := int64(len(req.Data)) + req.Offset; secondOffset < int64(len(f.Data)) {
		newData = append(newData, f.Data[secondOffset:]...)
	}
	f.Data = newData
	f.Size = uint64(len(f.Data))
	f.NeedsUpload = true
	log.Println("Wrote to file locally", f.FullPath)
	return nil
}
func (f *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	log.Println("Flushing file", f.FullPath)
	return nil
}
func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	log.Println("Open call on file", f.FullPath)
	f.PopulateFile()
	return f, nil
}

func (f *File) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	log.Println("Release requested on file", f.FullPath)
	if f.NeedsUpload {
		log.Println("Uploading file to Dropbox", f.FullPath)
		r := bytes.NewReader(f.Data)
		if err := f.Client.Upload(f.FullPath, r); err != nil {
			log.Panicln("Unable to upload file", f.FullPath, err)
		}
		f.NeedsUpload = false
		f.LastRefreshed = time.Now()
	}

	return nil
}

func (f *File) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	log.Println("Fsync call on file", f.FullPath)
	return nil
}
