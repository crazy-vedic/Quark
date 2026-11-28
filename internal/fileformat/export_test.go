package fileformat

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// seed builds API (two requests, an environment) with a nested Users
// collection holding a request whose name clashes with a sibling's slug.
func seed(t *testing.T, st *store.Store) *domain.Collection {
	t.Helper()
	ctx := context.Background()
	api := &domain.Collection{Name: "API", Description: "Main API"}
	require.NoError(t, st.SaveCollection(ctx, api))
	users := &domain.Collection{Name: "Users", ParentID: api.ID}
	require.NoError(t, st.SaveCollection(ctx, users))

	requests := []*domain.Request{
		{
			CollectionID: api.ID, Name: "Create item", Method: "POST",
			URL:      "{{baseUrl}}/items",
			Headers:  `{"Content-Type":"application/json","X-Tag":["one","two"]}`,
			Body:     "{\n  \"name\": \"widget\",\n  \"count\": 2\n}\n",
			AuthType: domain.AuthTypeBearer,
			AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
				Token: "{{token}}",
			}),
			SortOrder: 0, Enabled: true,
		},
		{
			CollectionID: api.ID, Name: "List items", Method: "GET",
			URL:      "{{baseUrl}}/items?limit=10",
			AuthType: domain.AuthTypeAPIKey,
			AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
				Name: "X-Key", Value: "secret", In: domain.AuthAPIKeyInQuery,
			}),
			SortOrder: 1, Enabled: true,
		},
		{
			CollectionID: users.ID, Name: "Get user", Method: "GET",
			URL: "{{baseUrl}}/users/1", Headers: `{"Accept":"*/*"}`,
			AuthType: domain.AuthTypeBasic,
			AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
				Username: "u", Password: "p",
			}),
			Enabled: true,
		},
		{
			// Disabled requests keep their switch off in the export.
			CollectionID: users.ID, Name: "get USER", Method: "DELETE",
			URL: "{{baseUrl}}/users/1", Body: "  leading spaces\ntrailing \n",
			SortOrder: 1,
		},
	}
	for _, req := range requests {
		require.NoError(t, st.SaveRequest(ctx, req))
	}
	envs, err := st.ListCollectionEnvironments(ctx, api.ID)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	envs[0].SetVars(map[string]string{"baseUrl": "https://api.example.com"})
	require.NoError(t, st.SaveEnvironment(ctx, envs[0]))
	staging := &domain.Environment{CollectionID: api.ID, Name: "Staging"}
	staging.SetVars(map[string]string{"baseUrl": "https://staging.example.com", "token": "t"})
	require.NoError(t, st.SaveEnvironment(ctx, staging))
	return api
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		require.NoError(t, err)
		data, err := os.ReadFile(path) //nolint:gosec // test temp dir
		require.NoError(t, err)
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	}))
	return files
}

func TestWriteProducesStableReadableLayout(t *testing.T) {
	st := newStore(t)
	api := seed(t, st)
	tree, err := Load(context.Background(), st, api.ID)
	require.NoError(t, err)

	out := t.TempDir()
	path, err := Write(out, tree)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(out, "api"), path)

	files := readTree(t, path)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	assert.ElementsMatch(t, []string{
		"collection.yaml",
		"create-item.yaml",
		"list-items.yaml",
		"environments/default.yaml",
		"environments/staging.yaml",
		"users/collection.yaml",
		"users/get-user.yaml",
		"users/get-user-2.yaml",
	}, names)

	assert.Equal(t, `format: 1
name: API
description: Main API
requests:
  - create-item.yaml
  - list-items.yaml
collections:
  - users
environments:
  - staging.yaml
  - default.yaml
`, files["collection.yaml"])
	assert.Equal(t, `name: Create item
method: POST
url: '{{baseUrl}}/items'
headers:
  Content-Type: application/json
  X-Tag:
    - one
    - two
auth:
  type: bearer
  token: '{{token}}'
body: |
  {
    "name": "widget",
    "count": 2
  }
`, files["create-item.yaml"])
	assert.Contains(t, files["list-items.yaml"], "  in: query\n")
	assert.NotContains(t, files["users/get-user.yaml"], "in:")
	assert.NotContains(t, files["users/get-user.yaml"], "disabled")
	assert.Contains(t, files["users/get-user-2.yaml"], "\ndisabled: true\n")
	assert.Equal(t, `name: Staging
variables:
  baseUrl: https://staging.example.com
  token: t
`, files["environments/staging.yaml"])

	// A second export of unchanged data is byte-for-byte identical.
	again, err := Write(out, tree)
	require.NoError(t, err)
	assert.Equal(t, files, readTree(t, again))
}

func TestWriteReplacesPreviousExportButNotOtherDirectories(t *testing.T) {
	out := t.TempDir()
	tree := &Collection{Name: "API", Requests: []*domain.Request{{Name: "A", Method: "GET"}}}
	path, err := Write(out, tree)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(path, "a.yaml"))

	tree.Requests = []*domain.Request{{Name: "B", Method: "GET"}}
	_, err = Write(out, tree)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(path, "a.yaml"), "deleted requests disappear")
	assert.FileExists(t, filepath.Join(path, "b.yaml"))

	other := &Collection{Name: "Notes"}
	require.NoError(t, os.MkdirAll(filepath.Join(out, "notes"), 0o750))
	_, err = Write(out, other)
	require.ErrorContains(t, err, "not a Quark export")

	require.NoError(t, os.WriteFile(filepath.Join(out, "file"), nil, 0o600))
	_, err = Write(out, &Collection{Name: "file"})
	require.ErrorContains(t, err, "not a directory")
}

func TestWriteKeepsUnparsableHeadersAndAuth(t *testing.T) {
	tree := &Collection{Name: "Broken", Requests: []*domain.Request{{
		Name: "Bad", Method: "GET", Headers: "not json",
		AuthType: "bearer", AuthConfig: "{oops",
	}}}
	path, err := Write(t.TempDir(), tree)
	require.NoError(t, err)
	back, err := Read(path)
	require.NoError(t, err)
	require.Len(t, back.Requests, 1)
	assert.Equal(t, "not json", back.Requests[0].Headers)
	assert.Equal(t, "{oops", back.Requests[0].AuthConfig)
}

func TestLoadErrors(t *testing.T) {
	st := newStore(t)
	_, err := Load(context.Background(), st, "missing")
	require.ErrorContains(t, err, "not found")
}

func TestSlug(t *testing.T) {
	assert.Equal(t, "get-user-by-id", slug("  Get user (by ID)! "))
	assert.Equal(t, "item", slug("!!!"))
	assert.Equal(t, "café", slug("Café"))
	assert.Len(t, []rune(slug(strings.Repeat("a", 100))), maxSlugRunes)
}

func writeUserFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, data := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(data), 0o600))
	}
}

func TestWriteRemovesOnlyFilesOfThePreviousExport(t *testing.T) {
	out := t.TempDir()
	first := &Collection{
		Name: "API",
		Requests: []*domain.Request{
			{Name: "Old name", Method: "GET"},
			{Name: "Kept", Method: "GET"},
		},
		Children: []*Collection{
			{Name: "Users", Requests: []*domain.Request{{Name: "Get user", Method: "GET"}}},
			{Name: "Admin", Requests: []*domain.Request{{Name: "Ping", Method: "GET"}}},
		},
		Environments: []Environment{{Name: "Staging", Vars: map[string]string{"a": "b"}}},
	}
	path, err := Write(out, first)
	require.NoError(t, err)

	// Files the user put in the export directory themselves.
	userFiles := map[string]string{
		"README.md":               "my notes\n",
		".git/config":             "[core]\n\trepositoryformatversion = 0\n",
		"users/notes.txt":         "check the admin role\n",
		"environments/.env.local": "SECRET=1\n",
	}
	writeUserFiles(t, path, userFiles)

	// Rename a request, drop the Admin sub-collection and the Staging environment.
	second := &Collection{
		Name: "API",
		Requests: []*domain.Request{
			{Name: "New name", Method: "GET"},
			{Name: "Kept", Method: "GET"},
		},
		Children: []*Collection{
			{Name: "Users", Requests: []*domain.Request{{Name: "Get user", Method: "GET"}}},
		},
	}
	_, err = Write(out, second)
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(path, "old-name.yaml"), "renamed request is removed")
	assert.FileExists(t, filepath.Join(path, "new-name.yaml"))
	assert.FileExists(t, filepath.Join(path, "kept.yaml"))
	assert.NoFileExists(t, filepath.Join(path, "admin", "collection.yaml"),
		"file of a removed sub-collection is removed")
	assert.NoFileExists(t, filepath.Join(path, "admin", "ping.yaml"))
	assert.NoDirExists(t, filepath.Join(path, "admin"), "emptied directory is removed")
	assert.NoFileExists(t, filepath.Join(path, "environments", "staging.yaml"),
		"file of a removed environment is removed")

	// Everything the user added survives, and so do the directories holding it.
	for rel, data := range userFiles {
		full := filepath.Join(path, filepath.FromSlash(rel))
		got, err := os.ReadFile(full) //nolint:gosec // test temp dir
		require.NoError(t, err, rel)
		assert.Equal(t, data, string(got), rel)
	}
	assert.FileExists(t, filepath.Join(path, "users", "get-user.yaml"))
}

func TestWriteRefusesPreviousMetadataThatNamesFilesOutsideTheExport(t *testing.T) {
	out := t.TempDir()
	first := &Collection{Name: "API", Requests: []*domain.Request{{Name: "A", Method: "GET"}}}
	path, err := Write(out, first)
	require.NoError(t, err)

	outside := filepath.Join(out, "outside.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("keep me\n"), 0o600))
	meta := "format: 1\nname: API\nrequests:\n  - ../outside.yaml\n"
	require.NoError(t, os.WriteFile(filepath.Join(path, "collection.yaml"), []byte(meta), 0o600))

	_, err = Write(out, &Collection{Name: "API"})
	require.Error(t, err)
	assert.FileExists(t, outside, "files outside the export are never removed")
}
