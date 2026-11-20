package atomicfile_test

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/atomicfile"
)

func TestWriteFile_CreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	require.NoError(t, atomicfile.WriteFile(path, []byte("one"), 0o600))
	require.NoError(t, atomicfile.WriteFile(path, []byte("two"), 0o600))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "two", string(got))
}

func TestWriteFile_ModeAndNoTempLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644)) //nolint:gosec // loose on purpose
	require.NoError(t, atomicfile.WriteFile(path, []byte("new"), 0o600))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestWrite_FillErrorKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(path, []byte("keep"), 0o600))
	boom := errors.New("boom")
	err := atomicfile.Write(path, 0o600, func(w io.Writer) error {
		_, _ = w.Write([]byte("partial"))
		return boom
	})
	require.ErrorIs(t, err, boom)
	got, _ := os.ReadFile(path)
	assert.Equal(t, "keep", string(got))
	entries, _ := os.ReadDir(dir)
	assert.Len(t, entries, 1, "temp file must be removed")
}

func TestWriteFile_MissingDirFails(t *testing.T) {
	err := atomicfile.WriteFile(filepath.Join(t.TempDir(), "no", "f"), nil, 0o600)
	assert.Error(t, err)
}

func TestWriteFile_FollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	require.NoError(t, os.WriteFile(real, []byte("old"), 0o644)) //nolint:gosec // fixture
	require.NoError(t, os.Symlink(real, link))
	require.NoError(t, atomicfile.WriteFile(link, []byte("new"), 0o644))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "link must survive")
	got, _ := os.ReadFile(real)
	assert.Equal(t, "new", string(got))
}

// forbidden are the calls that modify files without going through atomicfile.
// os.OpenFile is flagged whatever its flags: reads use os.Open.
var forbidden = map[string]bool{
	"os.WriteFile":     true,
	"os.Create":        true,
	"os.OpenFile":      true,
	"os.CreateTemp":    true,
	"os.Rename":        true,
	"os.Truncate":      true,
	"os.Chmod":         true,
	"ioutil.WriteFile": true,
	"ioutil.TempFile":  true,
}

// allowed lists the direct uses that cannot be an atomic whole-file replace.
// Key: "<slash path>:<function>:<call>". Each needs a reason; add sparingly.
var allowed = map[string]string{
	"cmd/quark/main.go:openDebugLogAt:os.Rename":           "archives the previous append-only debug log",
	"cmd/quark/main.go:openDebugLogAt:os.OpenFile":         "append-only debug log, written continuously for the whole session",
	"internal/store/backup.go:snapshotTo:os.OpenFile":      "pre-creates the 0600 destination VACUUM INTO writes into; it is removed on failure",
	"internal/store/backup.go:syncFile:os.OpenFile":        "fsync of the file VACUUM INTO produced; no content written",
	"internal/exec/executor.go:readResponse:os.CreateTemp": "scratch file for an oversized response body, not a replacement of a user file",
	"internal/exec/executor.go:readResponse:os.Chmod":      "restricts that scratch file to 0600",
	"internal/updates/channel.go:isWritable:os.CreateTemp": "probe: is the directory writable",
	"internal/updates/selfupdate.go:Update:os.Rename":      "Windows only: parks the backup as .old for lazy cleanup; moves a file, writes no content",
	"internal/updates/selfupdate.go:replace:os.Rename":     "installs the staged binary; rename is the atomic step (Windows needs rename-aside)",
}

// TestNoFileWritesBypassAtomicfile fails when non-test code outside this
// package modifies files directly. Use atomicfile.Write / WriteFile, or, if
// it truly cannot be a whole-file replace, add an entry to allowed with a reason.
func TestNoFileWritesBypassAtomicfile(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || rel == ".claude" || rel == "node_modules" ||
				rel == "internal/atomicfile" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, perr, rel)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || !forbidden[pkg.Name+"."+sel.Sel.Name] {
					return true
				}
				key := rel + ":" + fn.Name.Name + ":" + pkg.Name + "." + sel.Sel.Name
				if _, ok := allowed[key]; ok {
					used[key] = true
					return true
				}
				t.Errorf("%s: %s modifies a file directly; use atomicfile",
					fset.Position(call.Pos()), pkg.Name+"."+sel.Sel.Name)
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)

	for key := range allowed {
		if !used[key] {
			t.Errorf("stale allowlist entry %q: remove it", key)
		}
	}
}
