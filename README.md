# dropboxfs

A [FUSE](https://github.com/libfuse/libfuse) for seamlessly interacting with Dropbox written in Go. Dropboxfs allows you to mount your Dropbox and use it as if it was a local filesystem. All changes made through dropboxfs will be sync'ed with Dropbox. 

### How to run it

[Register an app with Dropbox](https://www.dropbox.com/developers/apps) and generate an access token for dropboxfs to use. Make sure your system has [FUSE](https://github.com/libfuse/libfuse) installed.

```
git clone https://github.com/Melinysh/dropboxfs.git
cd dropboxfs
go get .
go build
export DROPBOX_ACCESS_TOKEN=<Access Token Here>
./dropboxfs <MountPoint>
```

### TODO's
- [x] Read directories 
- [x] Creating directories
- [x] Removing directories 
- [x] Moving directories   
- [x] Read files  
- [x] Write files  
- [x] Create files  
- [x] Delete files 
- [x] Copy files
- [x] Rename files 
- [ ] Add backgrounding option  
- [ ] Stop spitting out so much debug info  
- [ ] Cleanup code  
- [ ] Better caching mechanism  
- [ ] Detect off-machine Dropbox changes and sync them   
- [ ] Add in better mechanism for getting/generating access tokens 
- [x] Add tests 

### License
MIT License

Copyright (c) 2018 Stephen Melinyshyn

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
