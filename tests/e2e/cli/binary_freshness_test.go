//go:build e2e

package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoRoot returns the module root, two levels above tests/e2e/cli.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return filepath.Join(wd, "..", "..", "..")
}

// newestSourceModTime returns the latest modification time of any Go source
// or module file under root. Build output and VCS metadata are skipped.
func newestSourceModTime(t *testing.T, root string) time.Time {
	t.Helper()
	var newest time.Time
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "dist", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		name := d.Name()
		if filepath.Ext(name) != ".go" && name != "go.mod" && name != "go.sum" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	require.NoError(t, err)
	return newest
}

// The CLI e2e tests run the built binary, which `go test` does not rebuild.
// A stale binary gives results for old code, so it must never be older than the sources.
func TestCLI_BinaryIsNotOlderThanSource(t *testing.T) {
	info, err := os.Stat(binaryPath)
	require.NoError(t, err, "quark binary missing at %s", binaryPath)
	newest := newestSourceModTime(t, repoRoot(t))
	assert.False(t, newest.After(info.ModTime()),
		"%s (built %s) is older than a source file (%s); the e2e tests would run stale code",
		binaryPath, info.ModTime(), newest)
}
