package main

import (
	"log"
	"os"

	dropbox "github.com/tj/go-dropbox"

	"golang.org/x/net/context"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

type Directory struct {
	*Node
	Subdirectories []*Node
	Files          []*Node
}

func (d *Directory) PopulateDirectory() {
	if !d.NeedsSync {
		log.Println("Directory", d.PathDisplay, "cached. Not fetching.")
		return
	}
	files, err := d.Client.ListFiles(d.PathDisplay)
	if err != nil {
		log.Panicln("Unable to load directories at path", d.PathDisplay)
	}
	folders, err := d.Client.ListFolders(d.PathDisplay)
	if err != nil {
		log.Panicln("Unable to load files at path", d.PathDisplay)
	}
	d.Files = files
	d.Subdirectories = folders
	d.NeedsSync = false
	log.Println("Populated directory at path", d.PathDisplay)
}

func (d *Directory) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Println("Requested Attr for Directory", d.PathDisplay)
	a.Inode = d.Inode
	a.Mode = os.ModeDir | 0700
	return nil
}

func (d *Directory) Lookup(ctx context.Context, name string) (fs.Node, error) {
	log.Println("Requested lookup for ", name)
	d.PopulateDirectory()
	for _, n := range d.Files {
		if n.Name == name {
			log.Println("Found match for file lookup with size", n.Size)
			return &File{
				Node: n,
			}, nil
		}
	}
	for _, n := range d.Subdirectories {
		if n.Name == name {
			log.Println("Found match for directory lookup")
			return &Directory{
				Node: n,
			}, nil
		}
	}
	return nil, fuse.ENOENT
}

func (d *Directory) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Println("Reading all dirs")
	d.PopulateDirectory()
	var children []fuse.Dirent
	for _, f := range d.Files {
		children = append(children, fuse.Dirent{Inode: f.Inode, Type: fuse.DT_File, Name: f.Name})
	}
	for _, dir := range d.Subdirectories {
		children = append(children, fuse.Dirent{Inode: dir.Inode, Type: fuse.DT_Dir, Name: dir.Name})
	}
	log.Println(len(children), " children for dir", d.PathDisplay)
	return children, nil
}

func (d *Directory) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	log.Println("Create request for name", req.Name)
	newFile := File{
		Node: &Node{
			dropbox.Metadata{Name: req.Name, PathDisplay: d.PathDisplay + "/" + req.Name},
			NewInode(),
			d.Client,
			false,
		},
	}
	fileMetadata, err := d.Client.Upload(newFile.PathDisplay, []byte{})
	if err != nil {
		log.Panicln("Unable to create file ", newFile.PathDisplay, err)
	}
	newFile.Node.Metadata = fileMetadata
	d.Files = append(d.Files, newFile.Node)
	return &newFile, &newFile, nil
}

func (d *Directory) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	log.Println("Rename request for", req.OldName, "to", req.NewName)
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
		newDirs := []*Node{}
		movingDir := &Node{}
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
		movingDir.PathDisplay = newParentDir.PathDisplay + "/" + req.NewName
		newPath = newParentDir.PathDisplay + req.NewName
		newParentDir.Subdirectories = append(newParentDir.Subdirectories, movingDir)
		movingDir.NeedsSync = true
	} else { // Remove file
		newFiles := []*Node{}
		movingFile := &Node{}
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
		movingFile.PathDisplay = newParentDir.PathDisplay + "/" + req.NewName
		newPath = movingFile.PathDisplay
		newParentDir.Files = append(newParentDir.Files, movingFile)
		movingFile.NeedsSync = true
	}
	newParentDir.NeedsSync = true
	d.NeedsSync = true

	_, err := d.Client.Move(oldPath, newPath)
	if err != nil {
		log.Panicln("Unable to move form oldPath", oldPath, "to new path", newPath, err)
	}

	return nil
}

func (d *Directory) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	log.Println("Remove request for ", req.Name)
	if req.Dir {
		newDirs := []*Node{}
		for _, dir := range d.Subdirectories {
			if dir.Name != req.Name {
				newDirs = append(newDirs, dir)
			}
		}
		d.Subdirectories = newDirs
	} else { // Remove file
		newFiles := []*Node{}
		for _, f := range d.Files {
			if f.Name != req.Name {
				newFiles = append(newFiles, f)
			}
		}
		d.Files = newFiles
	}
	_, err := d.Client.Delete(d.PathDisplay + "/" + req.Name)
	if err != nil {
		log.Panicln("Unable to delete item at path", d.PathDisplay+"/"+req.Name, err)
	}

	return nil
}

func (d *Directory) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	log.Println("Mkdir request for name", req.Name)

	if err := d.Client.Mkdir(d.PathDisplay + req.Name); err != nil {
		log.Panicln("Unable to create new directory at path", d.PathDisplay+req.Name, err)
	}
	newDir := Directory{
		Node: &Node{
			Metadata:  dropbox.Metadata{Name: req.Name, PathDisplay: d.PathDisplay + "/" + req.Name},
			Inode:     NewInode(),
			NeedsSync: false,
			Client:    d.Client,
		},
	}
	d.Subdirectories = append(d.Subdirectories, newDir.Node)
	return &newDir, nil
}
