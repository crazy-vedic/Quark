//go:build e2e

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/store"
)

// importDisabledHeaderFixture imports a collection with an active header, one
// with "disabled": false, one with "disabled": true and an empty-valued
// disabled one, and returns the command output and the stored headers.
func importDisabledHeaderFixture(
	t *testing.T,
	flags ...string,
) (out string, headers map[string]any) {
	t.Helper()
	home := t.TempDir()
	dbPath := createQuarkDBPath(t, home)
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	require.NoError(t, st.Close())

	exportDir := filepath.Join(t.TempDir(), "postman-export")
	collectionDir := filepath.Join(exportDir, "collection")
	require.NoError(t, os.MkdirAll(collectionDir, 0o700))
	writeJSONFile(t, filepath.Join(collectionDir, "headers.json"), map[string]any{
		"info": map[string]any{
			"name":   "Header API",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		"item": []map[string]any{{
			"name": "With headers",
			"request": map[string]any{
				"method": "GET",
				"url":    map[string]any{"raw": "https://api.example.test/things"},
				"header": []map[string]any{
					{"key": "X-Active", "value": "yes"},
					{"key": "X-Explicitly-On", "value": "also-yes", "disabled": false},
					{"key": "X-Switched-Off", "value": "stale-secret-value", "disabled": true},
					{"key": "X-Off-And-Empty", "value": "", "disabled": true},
				},
			},
		}},
	})

	args := append([]string{"import-postman", exportDir}, flags...)
	stdout, stderr, code := runQuarkWithHome(t, home, args...)
	require.Equal(t, 0, code, "import failed: %s %s", stdout, stderr)

	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	cols, err := st.ListCollections(ctx)
	require.NoError(t, err)

	var raw string
	for _, col := range cols {
		reqs, listErr := st.ListRequests(ctx, col.ID)
		require.NoError(t, listErr)
		for _, req := range reqs {
			if req.Name == "With headers" {
				raw = req.Headers
			}
		}
	}
	require.NotEmpty(t, raw, "the imported request must exist")
	require.NoError(t, json.Unmarshal([]byte(raw), &headers))
	return stdout + stderr, headers
}

// A header switched off in Postman is not sent there, so by default it is left
// out, and the import says so.
func TestE2E_PostmanImport_SkipsDisabledHeadersAndSaysSo(t *testing.T) {
	out, got := importDisabledHeaderFixture(t)

	assert.Contains(t, got, "X-Active")
	assert.Contains(t, got, "X-Explicitly-On")
	assert.NotContains(t, got, "X-Switched-Off", "a disabled header must not be imported")

	assert.Contains(t, out, `header "X-Switched-Off" is disabled in Postman and was not imported`)
	assert.Contains(t, out, "--include-disabled", "the warning names the flag that imports it")
	assert.NotContains(t, out, "stale-secret-value", "a header value is never printed")
	assert.NotContains(
		t,
		out,
		"X-Off-And-Empty",
		"an empty header would not have been imported anyway",
	)
	assert.NotContains(t, out, "X-Active", "only disabled headers are mentioned")
}

// --include-disabled imports them as active headers and tells the user that
// it did, since Quark cannot switch a header off.
func TestE2E_PostmanImport_IncludeDisabledImportsAndSaysSo(t *testing.T) {
	out, got := importDisabledHeaderFixture(t, "--include-disabled")

	assert.Contains(t, got, "X-Active")
	assert.Equal(t, "stale-secret-value", got["X-Switched-Off"], "the disabled header is imported")

	assert.Contains(
		t,
		out,
		`header "X-Switched-Off" is disabled in Postman but was imported as an active header`,
	)
	assert.NotContains(t, out, "was not imported", "nothing was dropped")
	assert.NotContains(t, out, "stale-secret-value", "a header value is never printed")
}
