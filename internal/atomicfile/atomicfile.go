// Package atomicfile is the one place Quark replaces a file's contents.
//
// Writing in place (os.WriteFile, os.Create, O_TRUNC) truncates the target
// first, so a crash, a full disk or a failed encode leaves a partial file. The
// helpers here write a temp file in the same directory, sync it, set its mode
// and rename it over the target, so readers see the old file or the new one.
//
// internal/atomicfile/atomicfile_test.go fails the build if any other non-test
// package modifies files directly.
package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFile atomically replaces path with data and perm.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return Write(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// Write atomically replaces path with whatever fill writes, with mode perm.
// If fill returns an error the target is left untouched and the temp file is
// removed. The parent directory must already exist. If path is a symlink the
// file it points to is replaced and the link is kept.
func Write(path string, perm os.FileMode, fill func(io.Writer) error) (err error) {
	target := resolve(path)
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	// Chmod before any content is written so secrets are never readable
	// under a looser mode. It also ignores the umask, unlike OpenFile.
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if err = fill(tmp); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(target), err)
	}
	syncDir(filepath.Dir(target))
	return nil
}

// resolve follows path if it is a symlink, so a dotfile managed as a link
// keeps being a link. A missing path is returned unchanged.
func resolve(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return path
	}
	return path
}

// syncDir persists the rename. Best effort: not supported on Windows.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
