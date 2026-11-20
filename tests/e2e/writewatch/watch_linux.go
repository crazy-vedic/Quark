//go:build e2e && linux

// Package writewatch records, with inotify, how files under a directory tree
// are written while a test runs, so an e2e test can prove that the binary
// replaces files atomically (temp file + rename) and never edits them in
// place. It is the runtime counterpart of the static check in
// internal/atomicfile.
package writewatch

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// tempName matches the temp files atomicfile.Write creates: ".<base>.<n>.tmp".
var tempName = regexp.MustCompile(`^\..+\.[0-9]+\.tmp$`)

const sentinel = ".writewatch-sentinel"

const mask = syscall.IN_MODIFY | syscall.IN_CLOSE_WRITE | syscall.IN_CREATE |
	syscall.IN_MOVED_TO | syscall.IN_MOVED_FROM | syscall.IN_DELETE | syscall.IN_ONLYDIR

// Watcher records file events below a root directory.
type Watcher struct {
	t      testing.TB
	root   string
	exempt []*regexp.Regexp
	f      *os.File

	mu       sync.Mutex
	dirs     map[int32]string // watch descriptor -> absolute dir
	inPlace  map[string]bool  // rel path -> written in place
	replaced map[string]bool  // rel path -> a file was renamed onto it
	done     chan struct{}
}

// Start watches root and every directory below it, including ones created
// later. exempt are regexps on slash-separated paths relative to root for
// files that are legitimately written in place (an SQLite database).
func Start(t testing.TB, root string, exempt ...string) *Watcher {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatalf("inotify_init1: %v", err)
	}
	w := &Watcher{
		t:        t,
		root:     root,
		f:        os.NewFile(uintptr(fd), "inotify"),
		dirs:     map[int32]string{},
		inPlace:  map[string]bool{},
		replaced: map[string]bool{},
		done:     make(chan struct{}),
	}
	for _, e := range exempt {
		w.exempt = append(w.exempt, regexp.MustCompile(e))
	}
	if err := w.addTree(root); err != nil {
		t.Fatalf("watch %s: %v", root, err)
	}
	go w.read()
	t.Cleanup(func() { _ = w.f.Close() })
	return w
}

// addTree watches dir and its subdirectories.
func (w *Watcher) addTree(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // created and removed before we got here
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		wd, err := syscall.InotifyAddWatch(int(w.f.Fd()), path, mask)
		if err != nil {
			return fmt.Errorf("inotify_add_watch %s: %w", path, err)
		}
		w.mu.Lock()
		w.dirs[int32(wd)] = path //nolint:gosec // G115: watch descriptors are small kernel ints
		w.mu.Unlock()
		return nil
	})
}

func (w *Watcher) read() {
	defer close(w.done)
	buf := make([]byte, 64*1024)
	for {
		n, err := w.f.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+syscall.SizeofInotifyEvent <= n; {
			ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[off]))
			nameBytes := buf[off+syscall.SizeofInotifyEvent : off+syscall.SizeofInotifyEvent+int(ev.Len)]
			name := string(bytes.TrimRight(nameBytes, "\x00"))
			off += syscall.SizeofInotifyEvent + int(ev.Len)
			if w.handle(ev.Wd, ev.Mask, name) {
				return
			}
		}
	}
}

// handle records one event and reports whether it was the stop sentinel.
func (w *Watcher) handle(wd int32, m uint32, name string) bool {
	w.mu.Lock()
	dir := w.dirs[wd]
	w.mu.Unlock()
	if dir == "" || name == "" {
		return false
	}
	abs := filepath.Join(dir, name)
	rel, _ := filepath.Rel(w.root, abs)
	rel = filepath.ToSlash(rel)
	if rel == sentinel {
		return m&syscall.IN_CREATE != 0
	}
	if m&syscall.IN_ISDIR != 0 {
		if m&(syscall.IN_CREATE|syscall.IN_MOVED_TO) != 0 {
			_ = w.addTree(abs)
		}
		return false
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case m&syscall.IN_MOVED_TO != 0 && !tempName.MatchString(name):
		w.replaced[rel] = true
	case m&(syscall.IN_MODIFY|syscall.IN_CLOSE_WRITE) != 0:
		// A write to anything but an atomicfile temp file is an in-place edit.
		// IN_CLOSE_WRITE also catches a file opened for writing and closed
		// without writing (os.Create of an empty file, O_TRUNC).
		if !tempName.MatchString(name) && !w.isExempt(rel) {
			w.inPlace[rel] = true
		}
	}
	return false
}

func (w *Watcher) isExempt(rel string) bool {
	for _, re := range w.exempt {
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

// Report is what happened between Start and Stop.
type Report struct {
	// InPlace are files written without going through a temp file + rename.
	InPlace []string
	// Replaced are files a temp file was renamed onto.
	Replaced []string
}

// Stop drains pending events and returns the report. It creates a sentinel
// directory in root and waits until the reader sees it, so every event the kernel
// queued before Stop is accounted for.
func (w *Watcher) Stop() Report {
	w.t.Helper()
	if err := os.Mkdir(filepath.Join(w.root, sentinel), 0o700); err != nil {
		w.t.Fatalf("create sentinel: %v", err)
	}
	select {
	case <-w.done:
	case <-time.After(10 * time.Second):
		w.t.Fatalf("writewatch: timed out waiting for inotify events")
	}
	_ = os.Remove(filepath.Join(w.root, sentinel))

	w.mu.Lock()
	defer w.mu.Unlock()
	return Report{InPlace: keys(w.inPlace), Replaced: keys(w.replaced)}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// String formats the report for failure messages.
func (r Report) String() string {
	return fmt.Sprintf("in place: [%s]; replaced: [%s]",
		strings.Join(r.InPlace, ", "), strings.Join(r.Replaced, ", "))
}
