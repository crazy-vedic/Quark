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

// A header switched off in Postman is not sent there, so importing it as an
// active header would change what the request does.
func TestE2E_PostmanImport_SkipsDisabledHeaders(t *testing.T) {
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
					{"key": "X-Switched-Off", "value": "no", "disabled": true},
				},
			},
		}},
	})

	stdout, stderr, code := runQuarkWithHome(t, home, "import-postman", exportDir)
	require.Equal(t, 0, code, "import failed: %s %s", stdout, stderr)

	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	cols, err := st.ListCollections(ctx)
	require.NoError(t, err)

	var headers string
	for _, col := range cols {
		reqs, listErr := st.ListRequests(ctx, col.ID)
		require.NoError(t, listErr)
		for _, req := range reqs {
			if req.Name == "With headers" {
				headers = req.Headers
			}
		}
	}
	require.NotEmpty(t, headers, "the imported request must exist")

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(headers), &got))
	assert.Contains(t, got, "X-Active")
	assert.Contains(t, got, "X-Explicitly-On")
	assert.NotContains(t, got, "X-Switched-Off", "a disabled header must not be imported")
}
