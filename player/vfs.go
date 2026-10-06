package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// gameFiles holds the game's files under "game/": embedded in the normal build,
// read from the player's own copy in the byof build (gamefs_*.go). gameErr says
// why they could not be loaded; NewApp refuses to start then.
var gameFiles, gameDir, gameErr = loadGameFS()

// VFS serves the game files read-only. Anything the game creates or
// writes (RAMPART.HIS) lives in SaveDir and shadows the embedded copy. Files in
// MemOnly (RAMPART.CFG) never touch the disk: reads see the embedded copy through
// Transform, writes stay in memory and are handed to OnWrite on close.
type VFS struct {
	SaveDir   string
	Transform func(name string, data []byte) []byte // applied to every read-only open
	OnWrite   func(name string, data []byte)        // after a written file is closed
	MemOnly   map[string]bool
	embedded  fs.FS
	names     map[string]string // upper-case name -> embedded path
}

func NewVFS(saveDir string) *VFS {
	v := &VFS{SaveDir: saveDir, embedded: gameFiles, names: map[string]string{}, MemOnly: map[string]bool{}}
	ents, _ := fs.ReadDir(gameFiles, "game")
	for _, e := range ents {
		v.names[strings.ToUpper(e.Name())] = "game/" + e.Name()
	}
	if saveDir != "" {
		os.MkdirAll(saveDir, 0o755)
	}
	return v
}

func (v *VFS) savePath(name string) string {
	if v.SaveDir == "" {
		return ""
	}
	return filepath.Join(v.SaveDir, strings.ToUpper(name))
}

func (v *VFS) ReadAll(name string) ([]byte, error) {
	name = strings.ToUpper(name)
	if p := v.savePath(name); p != "" && !v.MemOnly[name] {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	if e, ok := v.names[name]; ok {
		return fs.ReadFile(v.embedded, e)
	}
	return nil, fs.ErrNotExist
}

type vfile struct {
	data    []byte // read-only content
	pos     int64
	f       *os.File // writable file in SaveDir
	name    string
	vfs     *VFS
	written bool
	mem     bool // writable in-memory file (MemOnly)
}

func (f *vfile) Read(b []byte) int {
	if f.f != nil {
		n, _ := io.ReadFull(f.f, b)
		return n
	}
	if f.pos >= int64(len(f.data)) {
		return 0
	}
	n := copy(b, f.data[f.pos:])
	f.pos += int64(n)
	return n
}

func (f *vfile) Write(b []byte) int {
	if f.mem {
		end := f.pos + int64(len(b))
		if end > int64(len(f.data)) {
			f.data = append(f.data, make([]byte, end-int64(len(f.data)))...)
		}
		copy(f.data[f.pos:], b)
		f.pos = end
		f.written = true
		return len(b)
	}
	if f.f != nil {
		n, _ := f.f.Write(b)
		f.written = true
		return n
	}
	return 0
}

func (f *vfile) SeekTo(off int64, whence int) int64 {
	if f.f != nil {
		p, _ := f.f.Seek(off, whence)
		return p
	}
	switch whence {
	case 0:
		f.pos = off
	case 1:
		f.pos += off
	case 2:
		f.pos = int64(len(f.data)) + off
	}
	if f.pos < 0 {
		f.pos = 0
	}
	return f.pos
}

func (f *vfile) Close() {
	if f.mem {
		if f.written && f.vfs.OnWrite != nil {
			f.vfs.OnWrite(f.name, f.data)
		}
		return
	}
	if f.f != nil {
		path := f.f.Name()
		f.f.Close()
		if f.written && f.vfs != nil && f.vfs.OnWrite != nil {
			if b, err := os.ReadFile(path); err == nil {
				f.vfs.OnWrite(f.name, b)
			}
		}
	}
}

func (v *VFS) Open(name string, write bool) (*vfile, error) {
	name = strings.ToUpper(name)
	if v.MemOnly[name] && write {
		data, err := v.ReadAll(name)
		if err != nil {
			return nil, err
		}
		if v.Transform != nil {
			data = v.Transform(name, data)
		}
		return &vfile{data: append([]byte(nil), data...), name: name, vfs: v, mem: true}, nil
	}
	if !write && v.Transform != nil {
		data, err := v.ReadAll(name)
		if err != nil {
			return nil, err
		}
		return &vfile{data: v.Transform(name, data), name: name}, nil
	}
	sp := v.savePath(name)
	if sp != "" {
		if _, err := os.Stat(sp); err == nil {
			flag := os.O_RDONLY
			if write {
				flag = os.O_RDWR
			}
			f, err := os.OpenFile(sp, flag, 0o644)
			if err == nil {
				return &vfile{f: f, name: name, vfs: v}, nil
			}
		}
	}
	e, ok := v.names[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	data, err := fs.ReadFile(v.embedded, e)
	if err != nil {
		return nil, err
	}
	if write && sp != "" {
		if err := os.WriteFile(sp, data, 0o644); err == nil {
			if f, err := os.OpenFile(sp, os.O_RDWR, 0o644); err == nil {
				return &vfile{f: f, name: name, vfs: v}, nil
			}
		}
	}
	return &vfile{data: data, name: name}, nil
}

func (v *VFS) Create(name string) (*vfile, error) {
	if up := strings.ToUpper(name); v.MemOnly[up] {
		return &vfile{name: up, vfs: v, mem: true}, nil
	}
	sp := v.savePath(name)
	if sp == "" {
		return nil, errors.New("no save dir")
	}
	f, err := os.Create(sp)
	if err != nil {
		return nil, err
	}
	return &vfile{f: f, name: strings.ToUpper(name), vfs: v}, nil
}
