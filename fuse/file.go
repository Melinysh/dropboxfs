package fuse

import (
	"log"
	"sync"

	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"

	"golang.org/x/net/context"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"bazil.org/fuse/fuseutil"
)

type File struct {
	Metadata    *files.FileMetadata
	Data        []byte
	NeedsUpload bool
	Client      *Dropbox
	sync.Mutex
}

// Lock assumed
func (f *File) populateFile() {
	if f.Client.IsFileCached(f) {
		log.Println("File", f.Metadata.PathDisplay, "cached. Not refreshing it.")
		return
	}
	data, err := f.Client.Download(f.Metadata.PathDisplay)
	if err != nil {
		log.Panicln("Unable to download file", f.Metadata.PathDisplay, err)
	}
	f.Data = data
	f.Metadata.Size = uint64(len(data))
	f.NeedsUpload = false
}

func (f *File) Attr(ctx context.Context, a *fuse.Attr) error {
	f.Lock()
	defer f.Unlock()
	log.Println("Requested Attr for File", f.Metadata.PathDisplay)
	a.Inode = Inode(f.Metadata.Id)
	a.Mode = 0700
	a.Size = f.Metadata.Size
	return nil
}

func (f *File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	f.Lock()
	defer f.Unlock()
	log.Println("Requested Read on File", f.Metadata.PathDisplay)
	f.populateFile()
	fuseutil.HandleRead(req, resp, f.Data)
	return nil
}

func (f *File) ReadAll(ctx context.Context) ([]byte, error) {
	f.Lock()
	defer f.Unlock()
	log.Println("Reading all of file", f.Metadata.PathDisplay)
	f.populateFile()
	return f.Data, nil
}

func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	f.Lock()
	defer f.Unlock()
	log.Println("Trying to write to ", f.Metadata.PathDisplay, "offset", req.Offset, "dataSize:", len(req.Data))
	resp.Size = len(req.Data)
	newData := f.Data[:req.Offset]
	newData = append(newData, req.Data...)
	if secondOffset := int64(len(req.Data)) + req.Offset; secondOffset < int64(len(f.Data)) {
		newData = append(newData, f.Data[secondOffset:]...)
	}
	f.Data = newData
	f.Metadata.Size = uint64(len(f.Data))
	f.NeedsUpload = true
	log.Println("Wrote to file locally", f.Metadata.PathDisplay)
	return nil
}
func (f *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	f.Lock()
	defer f.Unlock()
	log.Println("Flushing file", f.Metadata.PathDisplay)
	return nil
}
func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	f.Lock()
	defer f.Unlock()
	log.Println("Open call on file", f.Metadata.PathDisplay)
	f.populateFile()
	return f, nil
}

func (f *File) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	f.Lock()
	defer f.Unlock()
	log.Println("Release requested on file", f.Metadata.PathDisplay)
	if f.NeedsUpload {
		log.Println("Uploading file to Dropbox", f.Metadata.PathDisplay)
		_, err := f.Client.Upload(f.Metadata.PathDisplay, f.Data)
		if err != nil {
			log.Panicln("Unable to upload file", f.Metadata.PathDisplay, err)
		}
		f.NeedsUpload = false
	}

	return nil
}

func (f *File) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	f.Lock()
	defer f.Unlock()
	log.Println("Fsync call on file", f.Metadata.PathDisplay)
	return nil
}
