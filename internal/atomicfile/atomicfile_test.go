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

// forbidden maps an import path to the identifiers in it that create, write,
// truncate, rename, link or re-permission files. Any reference counts, not
// just a call: `w := os.WriteFile; w(...)` is caught too. Reads (os.Open,
// os.ReadFile) and removal are not listed.
var forbidden = map[string]map[string]bool{
	"os": set(
		"WriteFile", "Create", "OpenFile", "CreateTemp", "Rename", "Truncate",
		"Chmod", "Chown", "Lchown", "Chtimes", "Link", "Symlink", "CopyFS",
		"NewFile", "OpenRoot", "OpenInRoot",
	),
	"io/ioutil":                set("WriteFile", "TempFile"),
	"syscall":                  lowLevel,
	"golang.org/x/sys/unix":    lowLevel,
	"golang.org/x/sys/windows": lowLevel,
}

var lowLevel = set(
	"Open", "Openat", "Openat2", "Creat", "Write", "Pwrite", "Writev", "Pwritev",
	"Rename", "Renameat", "Renameat2", "Truncate", "Ftruncate", "Link", "Linkat",
	"Symlink", "Symlinkat", "Chmod", "Fchmod", "Fchmodat", "CreateFile",
	"MoveFile", "MoveFileEx", "WriteFile",
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// allowed lists the direct uses that cannot be an atomic whole-file replace.
// Key: "<slash path>:<enclosing func>:<import path>.<name>". Each needs a
// reason; add sparingly. Entries that no longer match fail the test.
var allowed = map[string]string{
	"cmd/quark/main.go:openDebugLogAt:os.Rename":   "archives the previous append-only debug log",
	"cmd/quark/main.go:openDebugLogAt:os.OpenFile": "append-only debug log, written for the whole session",
	"internal/store/backup.go:snapshotTo:os.OpenFile": "pre-creates the 0600 file VACUUM INTO " +
		"writes; removed on failure",
	"internal/store/backup.go:syncFile:os.OpenFile": "fsync of the file VACUUM INTO produced; " +
		"writes nothing",
	"internal/exec/executor.go:readResponse:os.CreateTemp": "scratch file for an oversized " +
		"response body, not a user file",
	"internal/exec/executor.go:readResponse:os.Chmod":      "restricts that scratch file to 0600",
	"internal/updates/channel.go:isWritable:os.CreateTemp": "probe: is the directory writable",
	"internal/updates/selfupdate.go:Update:os.Rename": "Windows only: parks the backup as " +
		".old; moves a file, writes no content",
	"internal/updates/selfupdate.go:replace:os.Rename": "installs the staged binary; the " +
		"rename is the atomic step (Windows needs rename-aside)",
	"tests/e2e/writewatch/watch_linux.go:Start:os.NewFile": "wraps the inotify fd the " +
		"e2e write watcher reads; not a regular file",
}

// finding is one forbidden reference: key is the allowlist key, pos where.
type finding struct {
	key string
	pos token.Position
	msg string
}

// scanFile reports every forbidden reference in file. Imports are resolved by
// path, so renaming the import (xos "os") does not hide a call, and a dot
// import of a watched package is itself a finding.
func scanFile(fset *token.FileSet, rel string, file *ast.File) []finding {
	local := map[string]string{} // local name -> import path
	var out []finding
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if _, watched := forbidden[path]; !watched {
			continue
		}
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." {
			out = append(out, finding{
				key: rel + ":<import>:" + path + ".",
				pos: fset.Position(imp.Pos()),
				msg: "dot import of " + path + " hides file writes from this check",
			})
			continue
		}
		local[name] = path
	}

	visit := func(root ast.Node, fn string) {
		ast.Inspect(root, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Obj != nil { // id.Obj != nil: a local variable, not a package
				return true
			}
			path, ok := local[id.Name]
			if !ok || !forbidden[path][sel.Sel.Name] {
				return true
			}
			ref := path + "." + sel.Sel.Name
			out = append(out, finding{
				key: rel + ":" + fn + ":" + ref,
				pos: fset.Position(sel.Pos()),
				msg: ref + " modifies files directly; use atomicfile",
			})
			return true
		})
	}
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			visit(fd, fd.Name.Name)
		} else {
			visit(decl, "<package>")
		}
	}
	return out
}

// TestNoFileWritesBypassAtomicfile fails when Go code outside this package
// modifies files directly. It covers every non-test .go file in the module,
// whatever its build tags or GOOS. Use atomicfile.Write / WriteFile, or, if it
// truly cannot be a whole-file replace, add an entry to allowed with a reason.
// The runtime counterpart is tests/e2e/cli/atomic_writes_linux_test.go.
func TestNoFileWritesBypassAtomicfile(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	fset := token.NewFileSet()
	scanned := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == ".git", rel == ".claude", rel == "internal/atomicfile",
				d.Name() == "node_modules", d.Name() == "testdata", d.Name() == "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		// Mode 0 parses every file regardless of build constraints.
		file, perr := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, perr, rel)
		scanned++
		for _, f := range scanFile(fset, rel, file) {
			if _, ok := allowed[f.key]; ok {
				used[f.key] = true
				continue
			}
			t.Errorf("%s: %s", f.pos, f.msg)
		}
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, scanned, 50, "the walk must reach the module's sources")

	for key := range allowed {
		if !used[key] {
			t.Errorf("stale allowlist entry %q: remove it", key)
		}
	}
}

// The guard is only as good as scanFile. Each bypass shape must be caught.
func TestScanFile_CatchesBypasses(t *testing.T) {
	cases := map[string]struct {
		src  string
		want []string
	}{
		"plain call": {
			`package p; import "os"; func f() { os.WriteFile("x", nil, 0) }`,
			[]string{"p.go:f:os.WriteFile"},
		},
		"aliased import": {
			`package p; import xos "os"; func f() { xos.Create("x") }`,
			[]string{"p.go:f:os.Create"},
		},
		"function value": {
			`package p; import "os"; func f() { w := os.WriteFile; _ = w }`,
			[]string{"p.go:f:os.WriteFile"},
		},
		"package-level value": {
			`package p; import "os"; var w = os.Rename`,
			[]string{"p.go:<package>:os.Rename"},
		},
		"dot import": {
			`package p; import . "os"; func f() { WriteFile("x", nil, 0) }`,
			[]string{"p.go:<import>:os."},
		},
		"syscall": {
			`package p; import "syscall"; func f() { syscall.Open("x", 0, 0) }`,
			[]string{"p.go:f:syscall.Open"},
		},
		"x/sys/unix": {
			`package p; import "golang.org/x/sys/unix"; func f() { unix.Renameat(0, "", 0, "") }`,
			[]string{"p.go:f:golang.org/x/sys/unix.Renameat"},
		},
		"ioutil": {
			`package p; import "io/ioutil"; func f() { ioutil.WriteFile("x", nil, 0) }`,
			[]string{"p.go:f:io/ioutil.WriteFile"},
		},
		"os.Root": {
			`package p; import "os"; func f() { r, _ := os.OpenRoot("."); r.Create("x") }`,
			[]string{"p.go:f:os.OpenRoot"},
		},
		"reads are fine": {
			`package p; import "os"; func f() { os.ReadFile("x"); os.Open("x"); os.Remove("x") }`,
			nil,
		},
		"local variable named os is not the package": {
			`package p; func f() { var os struct{ WriteFile int }; _ = os.WriteFile }`,
			nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "p.go", tc.src, 0)
			require.NoError(t, err)
			var got []string
			for _, f := range scanFile(fset, "p.go", file) {
				got = append(got, f.key)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
