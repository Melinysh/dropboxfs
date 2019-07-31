package fuse

import (
	"os"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
	"golang.org/x/net/context"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

type Directory struct {
	Metadata       *files.FolderMetadata
	Subdirectories []*files.FolderMetadata
	Files          []*files.FileMetadata
	Client         *Dropbox
	sync.Mutex
}

// lock assumed
func (d *Directory) populateDirectory() {
	if d.Client.IsDirectoryCached(d) {
		log.Debugln("Directory", d.Metadata.PathDisplay, "cached. Not fetching.")
		return
	}
	files, folders, err := d.Client.listFilesAndFolders(d)
	if err != nil {
		log.Panicln("Unable to load files and folders at path", d.Metadata.PathDisplay, err)
	}
	d.Lock()
	d.Files = files
	d.Subdirectories = folders
	d.Unlock()
	log.Infof("Populated directory at path %+v\n", d.Metadata)
}

func (d *Directory) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Debugln("Requested Attr for Directory", d.Metadata.PathDisplay)
	a.Inode = Inode(d.Metadata.Id)
	a.Mode = os.ModeDir | 0700
	return nil
}

func (d *Directory) Lookup(ctx context.Context, name string) (fs.Node, error) {
	log.Debugln("Requested lookup for ", name)
	d.populateDirectory()
	for _, n := range d.Files {
		if n.Metadata.Name == name {
			log.Infoln("Found match for file lookup with size", n.Size)
			return d.Client.NewOrCachedFile(n), nil
		}
	}
	for _, n := range d.Subdirectories {
		if n.Metadata.Name == name {
			log.Debugln("Found match for directory lookup")
			return d.Client.NewOrCachedDirectory(n), nil
		}
	}
	return nil, fuse.ENOENT
}

func (d *Directory) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Infoln("Reading all dir", d.Metadata.PathDisplay)
	d.populateDirectory()
	var children []fuse.Dirent
	for _, f := range d.Files {
		children = append(children, fuse.Dirent{Inode: Inode(f.Id), Type: fuse.DT_File, Name: f.Metadata.Name})
	}
	for _, dir := range d.Subdirectories {
		children = append(children, fuse.Dirent{Inode: Inode(dir.Id), Type: fuse.DT_Dir, Name: dir.Metadata.Name})
	}
	return children, nil
}

func (d *Directory) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	log.Infoln("Create request for name", req.Name)

	fileMetadata, err := d.Client.Upload(d.Metadata.PathDisplay+"/"+req.Name, []byte{})
	if err != nil {
		log.Panicln("Unable to create file ", d.Metadata.PathDisplay+"/"+req.Name, err)
	}
	newFile := d.Client.NewOrCachedFile(fileMetadata)
	d.Lock()
	d.Files = append(d.Files, newFile.Metadata)
	d.Unlock()
	return newFile, newFile, nil
}

func (d *Directory) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	d.Lock()
	defer d.Unlock()
	log.Infoln("Rename request for", req.OldName, "to", req.NewName)
	newParentDir, _ := newDir.(*Directory)

	// figure out if we're working on dir or file, because req doesn't give us this
	isDir := false
	for _, dir := range d.Subdirectories {
		if dir.Name == req.OldName {
			isDir = true
			break
		}
	}

	// populate these two for the Dropbox call
	oldPath := ""
	newPath := ""
	if isDir {
		newDirs := []*files.FolderMetadata{}
		movingDir := &files.FolderMetadata{}
		for _, dir := range d.Subdirectories {
			if dir.Name != req.OldName {
				newDirs = append(newDirs, dir)
			} else {
				movingDir = dir
			}
		}

		d.Subdirectories = newDirs
		movingDir.Name = req.NewName
		oldPath = movingDir.PathDisplay[:len(movingDir.PathDisplay)-1]
		movingDir.Metadata.PathDisplay = newParentDir.Metadata.PathDisplay + "/" + req.NewName
		newPath = newParentDir.Metadata.PathDisplay + req.NewName
		newParentDir.Subdirectories = append(newParentDir.Subdirectories, movingDir)
	} else { // Remove file
		newFiles := []*files.FileMetadata{}
		movingFile := &files.FileMetadata{}
		for _, f := range d.Files {
			if f.Name != req.OldName {
				newFiles = append(newFiles, f)
			} else {
				movingFile = f
			}
		}
		d.Files = newFiles
		movingFile.Name = req.NewName
		oldPath = movingFile.PathDisplay
		movingFile.Metadata.PathDisplay = newParentDir.Metadata.PathDisplay + "/" + req.NewName
		newPath = movingFile.Metadata.PathDisplay
		newParentDir.Files = append(newParentDir.Files, movingFile)
	}

	if _, err := d.Client.Move(oldPath, newPath); err != nil {
		log.Panicln("Unable to move form oldPath", oldPath, "to new path", newPath, err)
	}

	return nil
}

func (d *Directory) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	log.Infoln("Remove request for ", req.Name)
	if req.Dir {
		newDirs := []*files.FolderMetadata{}
		for _, dir := range d.Subdirectories {
			if dir.Name != req.Name {
				newDirs = append(newDirs, dir)
			}
		}
		d.Subdirectories = newDirs
	} else { // Remove file
		newFiles := []*files.FileMetadata{}
		for _, f := range d.Files {
			if f.Name != req.Name {
				newFiles = append(newFiles, f)
			}
		}
		d.Files = newFiles
	}
	_, err := d.Client.Delete(d.Metadata.PathDisplay + "/" + req.Name)
	if err != nil {
		log.Panicln("Unable to delete item at path", d.Metadata.PathDisplay+"/"+req.Name, err)
	}

	return nil
}

func (d *Directory) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	log.Infoln("Mkdir request for name", req.Name)
	folderMetadata, err := d.Client.Mkdir(d.Metadata.PathDisplay + "/" + req.Name)
	if err != nil {
		log.Panicln("Unable to create new directory at path", d.Metadata.PathDisplay+"/"+req.Name, err)
	}
	newDir := d.Client.NewOrCachedDirectory(folderMetadata)
	d.Subdirectories = append(d.Subdirectories, newDir.Metadata)
	return newDir, nil
}
