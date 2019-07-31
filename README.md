# dropboxfs

A [FUSE](https://github.com/libfuse/libfuse) for seamlessly interacting with Dropbox written in Go. Dropboxfs allows you to mount your Dropbox and use it as if it was a local filesystem. All changes made through dropboxfs will be sync'ed with Dropbox. The Dropbox [linux daemon](https://www.dropbox.com/install-linux) synchronizes your entire Dropbox folder before you can use it, but dropboxfs is smarter and only loads your data when it needs to.

### How to run it

[Register an app with Dropbox](https://www.dropbox.com/developers/apps) and generate an access token for dropboxfs to use. Make sure your system has [FUSE](https://github.com/libfuse/libfuse) installed.

```
git clone https://github.com/Melinysh/dropboxfs.git
cd dropboxfs
go get .
go build
./dropboxfs -m <MountPoint>
```

A small amount of metrics can be emitted during running operation using
`expvar` by executing with `-e` flag and then locally monitoring with `expvarmon`.

Full options available are specified if library is run without arguments.

### Warning

The dropboxfs creates a file called dropbox_token in the root of where
project is created by default to store the authentication token. It's ignored
in .gitignore but could leak secrets if folder is copied in full off to different
system. You can create your own token file and specify the token path to avoid
this behavior.

```
dropboxfs -t ~/.config/dropboxfs/$AUTH_TOKEN_FILE
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
- [x] Stop spitting out so much debug info (make my own logger package)
- [x] Add verbose flag
- [x] Cleanup code
- [x] Better caching mechanism
- [ ] Time based cache eviction?
- [x] Detect off-machine Dropbox changes ([~Webhooks~](https://www.dropbox.com/developers/reference/webhooks), [Longpoll](https://www.dropbox.com/developers/documentation/http/documentation#files-list_folder-longpoll))
- [ ] Finer grain control to sync only whats changed, once change detected (might not be able to with longpoll API)
- [x] Add in better mechanism for getting/generating access tokens
- [x] Add tests
- [ ] Allow for changing of permissions
- [ ] Implement data structure for storing files/folders as tree datastructure for easier verifiably correct evictions and additions.
- [x] Crashes leave the volume mounted :-/. Should cleanup
- [ ] Allow for running when token is created for "App Folder not for full Dropbox"
- [x] Retry on case of EOF on http reads
- [x] Setup golang stats or statsd or both
- [ ] Should file and dir lookups be something like rocksdb instead of inmem? And then use TTL or LRU process for eviction
- [ ] Store metadata in one kv and file data in another region (so attr can be looked up without reading full file content)
- [x] Smaller lock regions
- [ ] Parallelize the requests? They seem SUPER slow. Is this FUSE, Bazil's version, or Drobpoxfs implementation
- [ ] Fix memory retention issue (is it avoidable?)
- [ ] Implement worker pool for Bazil/fuse where FS is served, vs new go routine each time
- [ ] Examine go-fuse ecosystem to see if other libraries offer performance improvements
- [ ] Write behavior in Suture library to ensure it stays running
- [ ] Add way to walk one level of project lower than current to help it feel more performant
- [ ] Store cursor AND then scan folder structures one level deep, the fire off goroutines on each of the folders from the results, to recursively do the same. Do this from a worker pool to make it more contained when we start getting rate limited.
- [ ] Hold onto the recursive Cursor(s) for accurate playback
- [ ] Use tree based data structure? that's a valid representation of filesystem that would have good invalidation semantics

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
