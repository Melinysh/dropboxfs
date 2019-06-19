package fuse

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"

	"golang.org/x/net/context"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"bazil.org/fuse/fuseutil"
	"github.com/cenkalti/backoff"
)

type File struct {
	Metadata    *files.FileMetadata
	Data        []byte
	NeedsUpload bool
	Client      *Dropbox
	sync.Mutex
}

func (f *File) populateFile() {
	if f.Client.IsFileCached(f) {
		log.Infoln("File", f.Metadata.PathDisplay, "cached. Not refreshing it.")
		return
	}
	retryNotice := func(err error, duration time.Duration) {
		log.Errorf("Retrying %s in %s due to %s\n", f.Metadata.PathDisplay, err, duration)
	}
	err := backoff.RetryNotify(func() error {
		data, err := f.Client.Download(f.Metadata.PathDisplay)

		if err != nil {
			return err
		}

		f.Lock()
		f.setData(data)
		f.Metadata.Size = uint64(len(data))
		f.NeedsUpload = false
		f.Unlock()
		return nil
	}, backoff.NewExponentialBackOff(), retryNotice)

	if err != nil {
		// TODO: retry
		log.Panicln("Unable to download file and retries failed", f.Metadata.PathDisplay, err)
	}
}

func (f *File) getData() []byte {
	return f.Data
}

func (f *File) setData(data []byte) {
	f.Data = data
}

func (f *File) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Infoln("Requested Attr for File", f.Metadata.PathDisplay)
	a.Inode = Inode(f.Metadata.Id)
	// TODO: fetch Mode from Dropbox if available
	a.Mode = 0700
	a.Size = f.Metadata.Size
	return nil
}

func (f *File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	log.Infoln("Requested Read on File", f.Metadata.PathDisplay)
	f.populateFile()
	fuseutil.HandleRead(req, resp, f.getData())
	return nil
}

func (f *File) ReadAll(ctx context.Context) ([]byte, error) {
	log.Infoln("Reading all of file", f.Metadata.PathDisplay)
	f.populateFile()
	return f.getData(), nil
}

func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	log.Infoln("Trying to write to ", f.Metadata.PathDisplay, "offset", req.Offset, "dataSize:", len(req.Data))
	f.Lock()
	defer f.Unlock()
	oldData := f.getData()
	resp.Size = len(req.Data)
	newData := oldData[:req.Offset]
	newData = append(newData, req.Data...)
	if secondOffset := int64(len(req.Data)) + req.Offset; secondOffset < int64(len(oldData)) {
		newData = append(newData, oldData[secondOffset:]...)
	}
	f.setData(newData)
	f.Metadata.Size = uint64(len(newData))
	f.NeedsUpload = true
	log.Infoln("Wrote to file locally", f.Metadata.PathDisplay)
	return nil
}
func (f *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	log.Infoln("Flushing file", f.Metadata.PathDisplay)
	return nil
}
func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	log.Infoln("Open call on file", f.Metadata.PathDisplay)
	f.populateFile()
	return f, nil
}

// Can this be done asynchronously?
func (f *File) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	log.Infoln("Release requested on file", f.Metadata.PathDisplay)
	if f.NeedsUpload {
		// Entirely reckless
		go func() {
			log.Infoln("Uploading file to Dropbox", f.Metadata.PathDisplay)
			retryNotice := func(err error, duration time.Duration) {
				log.Errorf("Retrying %s in %s due to %s\n", f.Metadata.PathDisplay, err, duration)
			}
			err := backoff.RetryNotify(func() error {
				_, err := f.Client.Upload(f.Metadata.PathDisplay, f.getData())
				if err != nil {
					return err
				}
				return nil
			}, backoff.NewExponentialBackOff(), retryNotice)

			if err != nil {
				log.Panicln("Unable to upload file", f.Metadata.PathDisplay, err)
			}
			f.NeedsUpload = false
		}()
	}

	return nil
}

func (f *File) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	log.Infoln("Fsync call on file", f.Metadata.PathDisplay)
	return nil
}
