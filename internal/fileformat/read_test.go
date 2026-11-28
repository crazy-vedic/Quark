package fileformat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

func TestReadIncludesHandAddedFilesAfterListedOnes(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"collection.yaml":     "format: 1\nname: Hand\nrequests:\n  - z.yaml\n",
		"z.yaml":              "name: Zed\nmethod: post\nurl: http://z\n",
		"a.yaml":              "url: http://a\nheaders:\n  X-Num: 5\n",
		"notes.txt":           "ignored",
		"sub/collection.yaml": "name: Sub\n",
		"sub/r.yaml":          "name: R\nmethod: GET\nurl: http://r\n",
		"plain/readme.yaml":   "not a collection",
		"environments/x.yaml": "variables:\n  k: v\n",
	})
	c, err := Read(dir)
	require.NoError(t, err)
	assert.Equal(t, "Hand", c.Name)
	require.Len(t, c.Requests, 2)
	assert.Equal(t, "Zed", c.Requests[0].Name)
	assert.Equal(t, "POST", c.Requests[0].Method)
	assert.Equal(t, "a", c.Requests[1].Name, "name defaults to the file name")
	assert.Equal(t, "GET", c.Requests[1].Method)
	assert.JSONEq(t, `{"X-Num":"5"}`, c.Requests[1].Headers)
	require.Len(t, c.Children, 1)
	assert.Equal(t, "Sub", c.Children[0].Name)
	require.Len(t, c.Environments, 1)
	assert.Equal(t, Environment{Name: "x", Vars: map[string]string{"k": "v"}}, c.Environments[0])
}

func TestReadRejectsBadFiles(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown field": {
			"collection.yaml": "name: C\nrequests: [r.yaml]\n",
			"r.yaml":          "name: R\nmethd: GET\n",
		},
		"newer format": {"collection.yaml": "format: 99\nname: C\n"},
		"path in list": {"collection.yaml": "name: C\nrequests: [../x.yaml]\n"},
		"missing file": {"collection.yaml": "name: C\nrequests: [gone.yaml]\n"},
		"both header forms": {
			"collection.yaml": "name: C\n",
			"r.yaml":          "name: R\nheaders: {A: b}\nheaders_json: '{}'\n",
		},
		"nested header list": {
			"collection.yaml": "name: C\n",
			"r.yaml":          "name: R\nheaders:\n  A:\n    - [x]\n",
		},
		"header map": {
			"collection.yaml": "name: C\n",
			"r.yaml":          "name: R\nheaders:\n  A: {b: c}\n",
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, files)
			_, err := Read(dir)
			require.Error(t, err)
		})
	}
}

func TestReadEmptyCollectionFileUsesDirectoryName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "orders")
	writeFiles(t, dir, map[string]string{"collection.yaml": ""})
	c, err := Read(dir)
	require.NoError(t, err)
	assert.Equal(t, "orders", c.Name)
}

func TestFindCollections(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"b/collection.yaml":       "name: B\n",
		"a/collection.yaml":       "name: A\n",
		".hidden/collection.yaml": "name: H\n",
		"other/x.txt":             "",
	})
	dirs, err := FindCollections(root)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(root, "a"), filepath.Join(root, "b")}, dirs)

	dirs, err = FindCollections(filepath.Join(root, "a"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(root, "a")}, dirs)

	_, err = FindCollections(filepath.Join(root, "other"))
	require.ErrorContains(t, err, "no collection.yaml")
	_, err = FindCollections(filepath.Join(root, "missing"))
	require.Error(t, err)
}
