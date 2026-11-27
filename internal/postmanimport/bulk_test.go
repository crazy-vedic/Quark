package postmanimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeManifestID(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		valid bool
	}{
		{"empty", "", false},
		{"dot_dot", "..", false},
		{"dot_dot_slash", "../", false},
		{"leading_slash", "/collection", false},
		{"backslash", "back\\slash", false},
		{"forward_slash", "path/to/file", false},
		{"valid_id", "collection", true},
		{"valid_with_hyphen", "my-collection", true},
		{"valid_with_underscore", "my_collection", true},
		{"nested_traversal", "nested/../path", false},
		{"double_dots_in_middle", "foo/..bar", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, safeManifestID(tt.id))
		})
	}
}

func TestImportBulk_StandaloneEnvironmentsRemainSelectableAndAggregateErrors(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	global, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	global.SetVars(map[string]string{"existing": "global-secret"})
	require.NoError(t, st.SaveEnvironment(ctx, global))

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "collection"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "environment"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "collection", "collection.json"),
		[]byte(`{"info":{"name":"Imported","schema":"v2.1"},"item":[]}`),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "environment", "a.json"),
		[]byte(`{
			"name":"A",
			"values":[
				{"key":"shared","value":"first-secret","enabled":true},
				{"key":"existing","value":"import-secret","enabled":true}
			]
		}`),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "environment", "b.json"),
		[]byte(`{"name":"B","values":[{"key":"shared","value":"later-secret","enabled":true}]}`),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "environment", "g.json"),
		[]byte(`{"name":"G","scope":"global",`+
			`"values":[{"key":"existing","value":"import-secret","enabled":true}]}`),
		0o600,
	))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "environment", "z.json"), []byte(`not-json`), 0o600),
	)

	result, err := New(st, nil).Import(ctx, dir, Options{})
	require.NoError(t, err)
	assert.True(t, result.Bulk)
	require.Len(t, result.Collections, 1)
	assert.NoError(t, result.Collections[0].Err)
	envResult := result.Environments
	assert.Equal(t, 3, envResult.Imported, "one global file plus A and B on the collection")
	assert.Len(t, envResult.Errors, 1)
	merged, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	assert.Equal(t, "global-secret", merged.Vars()["existing"])
	assert.NotContains(t, merged.Vars(), "shared")
	collection, err := st.ResolveCollectionPath(ctx, "Imported")
	require.NoError(t, err)
	first, err := st.GetEnvironmentByName(ctx, collection.ID, "A")
	require.NoError(t, err)
	assert.Equal(t, "first-secret", first.Vars()["shared"])
	assert.Equal(t, "import-secret", first.Vars()["existing"])
	second, err := st.GetEnvironmentByName(ctx, collection.ID, "B")
	require.NoError(t, err)
	assert.Equal(t, "later-secret", second.Vars()["shared"])
	output := strings.Join(append(envResult.Warnings, envResult.Errors...), " ")
	for _, secret := range []string{"global-secret", "import-secret", "first-secret", "later-secret"} {
		assert.NotContains(t, output, secret)
	}
	var globalCount int
	require.NoError(
		t,
		st.DB().
			QueryRow(`SELECT COUNT(*) FROM environments WHERE collection_id IS NULL`).
			Scan(&globalCount),
	)
	assert.Equal(t, 1, globalCount)

	summary := result.Summary()
	assert.Equal(t, 1, summary.Created)
	assert.Equal(t, envResult.Imported, summary.Environments)
	assert.Equal(t, 1, summary.Errors)
}

func TestImportBulkManifestAndEmptyDirectoryPaths(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	service := New(st, nil)

	_, err := service.Import(ctx, t.TempDir(), Options{})
	require.ErrorContains(t, err, "missing collection/")

	emptyCollection := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(emptyCollection, "collection"), 0o700))
	_, err = service.Import(ctx, emptyCollection, Options{})
	require.ErrorContains(t, err, "no collection files")

	fallback := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(fallback, "collection"), 0o700))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(fallback, "archive.json"), []byte("not-json"), 0o600),
	)
	manifestJSON := `{"info":{"name":"Manifest fallback","schema":"v2.1"},"item":[]}`
	require.NoError(t, os.WriteFile(
		filepath.Join(fallback, "collection", "valid.json"), []byte(manifestJSON), 0o600))
	result, err := service.Import(ctx, fallback, Options{})
	require.NoError(t, err)
	require.Len(t, result.Collections, 1)
	require.NoError(t, result.Collections[0].Err)

	manifest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(manifest, "collection"), 0o700))
	manifestData := `{"collection":{"../unsafe":true,"safe":true,"missing":true}}`
	require.NoError(
		t,
		os.WriteFile(filepath.Join(manifest, "archive.json"), []byte(manifestData), 0o600),
	)
	safeData := `{"info":{"name":"Safe manifest","schema":"v2.1"},"item":[]}`
	require.NoError(t, os.WriteFile(
		filepath.Join(manifest, "collection", "safe.json"), []byte(safeData), 0o600))
	result, err = service.Import(ctx, manifest, Options{})
	require.NoError(t, err)
	require.Len(t, result.Collections, 1)
	require.NoError(t, result.Collections[0].Err)
}

func TestImportBulk_FailedCollectionIsReportedAndOthersImport(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "collection"), 0o700))
	writeCollectionFile(t, filepath.Join(dir, "collection"), "a.json", healthCollection)
	writeCollectionFile(t, filepath.Join(dir, "collection"), "b.json", "not json")

	result, err := New(st, nil).Import(ctx, dir, Options{})
	require.NoError(t, err)
	require.Len(t, result.Collections, 2)
	assert.NoError(t, result.Collections[0].Err)
	assert.ErrorContains(t, result.Collections[1].Err, "parse:")
	summary := result.Summary()
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.RequestsImported)
}
