package main

import (
	"bytes"
	"log"
	"os"
	"time"

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
	if time.Since(d.LastRefreshed) < 5*time.Minute {
		log.Println("Directory", d.FullPath, "cached until 5 minutes has passed")
		return
	}
	files, err := d.Client.ListFiles(d.FullPath)
	if err != nil {
		log.Panicln("Unable to load directories at path", d.FullPath)
	}
	folders, err := d.Client.ListFolders(d.FullPath)
	if err != nil {
		log.Panicln("Unable to load files at path", d.FullPath)
	}
	d.Files = NewNodes(files, d)
	d.Subdirectories = NewNodes(folders, d)
	d.LastRefreshed = time.Now()
	log.Println("Populated directory at path", d.FullPath)
}

func (d *Directory) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Println("Requested Attr for Directory", d.FullPath)
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
	log.Println(len(children), " children for dir", d.FullPath)
	return children, nil
}

func (d *Directory) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	log.Println("Create request for name", req.Name)
	newFile := File{
		Node: &Node{
			Inode:         NewInode(),
			FullPath:      d.FullPath + req.Name,
			LastRefreshed: time.Now(),
			Name:          req.Name,
			Client:        d.Client,
		},
	}
	r := bytes.NewReader([]byte{}) // empty
	if err := d.Client.Upload(newFile.FullPath, r); err != nil {
		log.Panicln("Unable to create file ", newFile.FullPath, err)
	}
	d.Files = append(d.Files, newFile.Node)
	d.LastRefreshed = time.Now()
	return &newFile, &newFile, nil
}

func (d *Directory) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	log.Println("Rename request for ", req.OldName, "to", req.NewName)
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
		oldPath = movingDir.FullPath
		movingDir.FullPath = newParentDir.FullPath + req.NewName + "/"
		newPath = movingDir.FullPath
		newParentDir.Subdirectories = append(newParentDir.Subdirectories, movingDir)
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
		oldPath = movingFile.FullPath
		movingFile.FullPath = newParentDir.FullPath + req.NewName
		newPath = movingFile.FullPath
		newParentDir.Files = append(newParentDir.Files, movingFile)
	}

	if err := d.Client.Move(oldPath, newPath); err != nil {
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

	if err := d.Client.Delete(d.FullPath + req.Name); err != nil {
		log.Panicln("Unable to delete item at path", d.FullPath+req.Name, err)
	}

	d.LastRefreshed = time.Now()
	return nil
}

func (d *Directory) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	log.Println("Mkdir request for name", req.Name)
	newDir := Directory{
		Node: &Node{
			Inode:         NewInode(),
			Name:          req.Name,
			FullPath:      d.FullPath + req.Name + "/",
			LastRefreshed: time.Now(),
			Client:        d.Client,
		},
	}

	if err := d.Client.Mkdir(d.FullPath + req.Name); err != nil {
		log.Panicln("Unable to create new directory at path", newDir.FullPath, err)
	}

	d.Subdirectories = append(d.Subdirectories, newDir.Node)
	d.LastRefreshed = time.Now()
	return &newDir, nil
}

/*func (d *Directory) Mknod(ctx context.Context, req *fuse.MknodRequest) (fs.Node, error) {
	log.Println("Mknode request for name", req.Name)

	newFile := File{
		Node: &Node{
			Inode:         NewInode(),
			Name:          req.Name,
			FullPath:      d.FullPath + req.Name,
			LastRefreshed: time.Now(),
			Client:        d.Client,
		},
		Data: []byte{},
	}
	r := bytes.NewReader([]byte{}) // empty
	if err := d.Client.Upload(newFile.FullPath, r); err != nil {
		log.Panicln("Unable to create file at path", newFile.FullPath, err)
	}

	d.Files = append(d.Files, newFile.Node)
	d.LastRefreshed = time.Now()
	return &newFile, nil
}*/
