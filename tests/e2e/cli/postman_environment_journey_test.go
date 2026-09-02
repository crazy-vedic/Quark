//go:build e2e

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestE2E_PostmanBulkImport_CompleteEnvironmentJourney(t *testing.T) {
	requests := make(chan observedRequest, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- observedRequest{path: r.URL.Path, query: r.URL.RawQuery, headers: r.Header.Clone()}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	dbPath := createQuarkDBPath(t, home)
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	global, err := st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	global.SetVars(map[string]string{
		"existing":  "local-existing-secret",
		"identical": "same-value",
	})
	require.NoError(t, st.SaveEnvironment(context.Background(), global))
	require.NoError(t, st.Close())

	exportDir := filepath.Join(t.TempDir(), "postman-export")
	collectionDir := filepath.Join(exportDir, "collection")
	environmentDir := filepath.Join(exportDir, "environment")
	require.NoError(t, os.MkdirAll(collectionDir, 0o700))
	require.NoError(t, os.MkdirAll(environmentDir, 0o700))

	collection := map[string]any{
		"info": map[string]any{
			"name":   "Imported API",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		"variable": []map[string]any{
			{"key": "base_url", "value": srv.URL},
			{"key": "shared", "value": "collection-default"},
			{"key": "root_only", "value": "root-default-only"},
			{"key": "number", "value": 42},
			{"key": "flag", "value": true},
			{"key": "nil_value", "value": nil},
			{"key": "duplicate", "value": "first-duplicate-secret"},
			{"key": "duplicate", "value": "later-duplicate-secret"},
			{"key": "", "value": "empty-key-secret"},
			{"key": "disabled_collection", "value": "disabled-collection-secret", "disabled": true},
		},
		"item": []map[string]any{{
			"name": "Users",
			"item": []map[string]any{{
				"name": "Admin",
				"item": []map[string]any{{
					"name": "Probe",
					"request": map[string]any{
						"method": "GET",
						"url":    map[string]any{"raw": "{{base_url}}/postman/{{shared}}?number={{number}}&flag={{flag}}&nil={{nil_value}}&duplicate={{duplicate}}&global={{env_first}}&root={{root_only}}&parent={{parent_only}}&child={{child_only}}"},
						"header": []map[string]any{{"key": "X-Shared", "value": "{{shared}}"}},
					},
				}},
			}},
		}},
	}
	writeJSONFile(t, filepath.Join(collectionDir, "collection.json"), collection)
	writeJSONFile(t, filepath.Join(environmentDir, "a-environment.json"), map[string]any{
		"id": "a", "name": "A Environment", "scope": "environment",
		"values": []map[string]any{
			{"key": "env_first", "value": "first-environment-secret", "enabled": true},
			{"key": "existing", "value": "incoming-overwrite-secret", "enabled": true},
			{"key": "identical", "value": "same-value", "enabled": true},
			{"key": "duplicate_in_file", "value": "first-within-file-secret", "enabled": true},
			{"key": "duplicate_in_file", "value": "later-within-file-secret", "enabled": true},
			{"key": "disabled_environment", "value": "disabled-environment-secret", "enabled": false},
			{"key": "", "value": "empty-environment-key-secret", "enabled": true},
		},
	})
	writeJSONFile(t, filepath.Join(environmentDir, "b-global.json"), map[string]any{
		"id": "b", "name": "B Global", "scope": "global",
		"values": []map[string]any{
			{"key": "env_first", "value": "later-environment-secret", "enabled": true},
			{"key": "unicode_变量", "value": "unicode-value", "enabled": true},
		},
	})
	writeJSONFile(t, filepath.Join(environmentDir, "c-unknown.json"), map[string]any{
		"id": "c", "name": "C Unknown", "scope": "future-scope",
		"values": []map[string]any{{"key": "unknown_scope_key", "value": "unknown-scope-value", "enabled": true}},
	})
	require.NoError(t, os.WriteFile(filepath.Join(environmentDir, "z-malformed.json"), []byte(`{"name":`), 0o600))

	stdout, stderr, code := runQuarkWithHome(t, home, "import-postman", exportDir)
	assert.NotEqual(t, 0, code, "malformed environment must make the final result nonzero")
	combined := stdout + stderr
	assert.Contains(t, combined, "Imported API")
	assert.Contains(t, combined, "z-malformed.json")
	assert.Contains(t, combined, "environment error")
	assert.Contains(t, combined, `variable "env_first"`)
	assert.Contains(t, combined, `duplicate collection variable "duplicate"`)
	assert.Contains(t, combined, "empty key")
	for _, secret := range []string{
		"first-environment-secret", "later-environment-secret", "incoming-overwrite-secret",
		"first-within-file-secret", "later-within-file-secret", "disabled-environment-secret",
		"first-duplicate-secret", "later-duplicate-secret", "empty-key-secret",
	} {
		assert.NotContains(t, combined, secret, "import diagnostics must redact values")
	}

	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	paths := collectionPaths(t, st)
	require.Contains(t, paths, "Imported API")
	require.Contains(t, paths, "Imported API/Users")
	require.Contains(t, paths, "Imported API/Users/Admin")

	root := paths["Imported API"]
	parent := paths["Imported API/Users"]
	child := paths["Imported API/Users/Admin"]
	rootDefault, err := st.GetEnvironmentByName(context.Background(), root.ID, "default")
	require.NoError(t, err)
	assert.Equal(t, srv.URL, rootDefault.Vars()["base_url"])
	assert.Equal(t, "42", rootDefault.Vars()["number"])
	assert.Equal(t, "true", rootDefault.Vars()["flag"])
	assert.Equal(t, "", rootDefault.Vars()["nil_value"])
	assert.Equal(t, "first-duplicate-secret", rootDefault.Vars()["duplicate"])
	assert.NotContains(t, rootDefault.Vars(), "disabled_collection")
	assert.NotContains(t, rootDefault.Vars(), "")

	for _, collection := range []*domain.Collection{parent, child} {
		childDefault, getErr := st.GetEnvironmentByName(context.Background(), collection.ID, "default")
		require.NoError(t, getErr)
		assert.Empty(t, childDefault.Vars(), "root collection variables must not be duplicated into descendants")
	}

	global, err = st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	globalVars := global.Vars()
	assert.Equal(t, "local-existing-secret", globalVars["existing"])
	assert.Equal(t, "same-value", globalVars["identical"])
	assert.Equal(t, "first-environment-secret", globalVars["env_first"])
	assert.Equal(t, "first-within-file-secret", globalVars["duplicate_in_file"])
	assert.Equal(t, "unicode-value", globalVars["unicode_变量"])
	assert.Equal(t, "unknown-scope-value", globalVars["unknown_scope_key"])
	assert.NotContains(t, globalVars, "disabled_environment")
	assert.NotContains(t, globalVars, "")

	var globalRows, environmentRows int
	require.NoError(t, st.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM environments WHERE collection_id IS NULL`).Scan(&globalRows))
	require.NoError(t, st.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM environments`).Scan(&environmentRows))
	assert.Equal(t, 1, globalRows)
	assert.Equal(t, 4, environmentRows, "standalone Postman environments must not create tabs or rows")

	parentDefault, err := st.GetEnvironmentByName(context.Background(), parent.ID, "default")
	require.NoError(t, err)
	childDefault, err := st.GetEnvironmentByName(context.Background(), child.ID, "default")
	require.NoError(t, err)
	parentDefault.SetVars(map[string]string{"parent_only": "parent-default-only", "shared": "parent-default"})
	childDefault.SetVars(map[string]string{"child_only": "child-default-only", "shared": "child-default"})
	require.NoError(t, st.SaveEnvironment(context.Background(), parentDefault))
	require.NoError(t, st.SaveEnvironment(context.Background(), childDefault))
	rootDev := saveNamedEnvironment(t, st, root.ID, "import-root-dev", "dev", map[string]string{"shared": "root-dev"})
	parentDev := saveNamedEnvironment(t, st, parent.ID, "import-parent-dev", "dev", map[string]string{"shared": "parent-dev"})
	childDev := saveNamedEnvironment(t, st, child.ID, "import-child-dev", "dev", map[string]string{"shared": "child-dev"})
	rootOther := saveNamedEnvironment(t, st, root.ID, "import-root-other", "other", map[string]string{"shared": "wrong-root-active"})
	parentOther := saveNamedEnvironment(t, st, parent.ID, "import-parent-other", "other", map[string]string{"shared": "wrong-parent-active"})
	require.NoError(t, st.SetActiveEnvironment(context.Background(), root.ID, rootOther.ID))
	require.NoError(t, st.SetActiveEnvironment(context.Background(), parent.ID, parentOther.ID))
	require.NoError(t, st.SetActiveEnvironment(context.Background(), child.ID, childDev.ID))
	_ = rootDev
	_ = parentDev
	require.NoError(t, st.Close())

	stdout, stderr, code = runQuarkWithHome(t, home, "run", "Imported API/Users/Admin/Probe")
	require.Equal(t, 0, code, "stdout=%s stderr=%s", stdout, stderr)
	select {
	case got := <-requests:
		assert.Equal(t, "/postman/child-dev", got.path)
		assert.Contains(t, got.query, "number=42")
		assert.Contains(t, got.query, "flag=true")
		assert.Contains(t, got.query, "nil=")
		assert.Contains(t, got.query, "duplicate=first-duplicate-secret")
		assert.Contains(t, got.query, "global=first-environment-secret")
		assert.Contains(t, got.query, "root=root-default-only")
		assert.Contains(t, got.query, "parent=parent-default-only")
		assert.Contains(t, got.query, "child=child-default-only")
		assert.Equal(t, "child-dev", got.headers.Get("X-Shared"))
	case <-time.After(3 * time.Second):
		t.Fatal("imported nested request was not dispatched")
	}
}

func TestE2E_PostmanCollectionVariables_DuplicateActions(t *testing.T) {
	actions := []struct {
		name         string
		action       string
		seedExisting bool
		targetName   string
		wantConflict string
		wantAdded    string
		wantRequests int
		wantRoots    int
	}{
		{name: "new", action: "duplicate", targetName: "API", wantConflict: "import-secret", wantAdded: "added-secret", wantRequests: 1, wantRoots: 1},
		{name: "duplicate", action: "duplicate", seedExisting: true, targetName: "API 1", wantConflict: "import-secret", wantAdded: "added-secret", wantRequests: 1, wantRoots: 2},
		{name: "replace", action: "replace", seedExisting: true, targetName: "API", wantConflict: "import-secret", wantAdded: "added-secret", wantRequests: 1, wantRoots: 1},
		{name: "merge", action: "merge", seedExisting: true, targetName: "API", wantConflict: "local-secret", wantAdded: "added-secret", wantRequests: 1, wantRoots: 1},
		{name: "skip", action: "skip", seedExisting: true, targetName: "API", wantConflict: "local-secret", wantAdded: "", wantRequests: 0, wantRoots: 1},
	}

	for _, tc := range actions {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			dbPath := createQuarkDBPath(t, home)
			st, err := store.New(dbPath, store.WithCacheSize(100))
			require.NoError(t, err)
			if tc.seedExisting {
				existing := &domain.Collection{ID: "existing-api", Name: "API"}
				require.NoError(t, st.SaveCollection(context.Background(), existing))
				defaultEnvironment, getErr := st.GetEnvironmentByName(context.Background(), existing.ID, "default")
				require.NoError(t, getErr)
				defaultEnvironment.SetVars(map[string]string{"conflict": "local-secret", "identical": "same-value"})
				require.NoError(t, st.SaveEnvironment(context.Background(), defaultEnvironment))
			}
			require.NoError(t, st.Close())

			collectionFile := filepath.Join(t.TempDir(), "api.postman_collection.json")
			writeJSONFile(t, collectionFile, map[string]any{
				"info": map[string]any{"name": "API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
				"variable": []map[string]any{
					{"key": "conflict", "value": "import-secret"},
					{"key": "identical", "value": "same-value"},
					{"key": "added", "value": "added-secret"},
				},
				"item": []map[string]any{{
					"name": "Request", "request": map[string]any{"method": "GET", "url": "https://example.test/{{added}}"},
				}},
			})

			stdout, stderr, code := runQuarkWithHome(t, home, "import-postman", collectionFile, "--on-duplicate", tc.action)
			require.Equal(t, 0, code, "stdout=%s stderr=%s", stdout, stderr)
			combined := stdout + stderr
			assert.NotContains(t, combined, "local-secret")
			assert.NotContains(t, combined, "import-secret")
			assert.NotContains(t, combined, "added-secret")
			if tc.action == "merge" {
				assert.Contains(t, combined, `collection variable "conflict" conflicts`)
			}

			st, err = store.New(dbPath, store.WithCacheSize(100))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, st.Close()) })
			paths := collectionPaths(t, st)
			assert.Len(t, paths, tc.wantRoots)
			target := paths[tc.targetName]
			require.NotNil(t, target)
			defaultEnvironment, getErr := st.GetEnvironmentByName(context.Background(), target.ID, "default")
			require.NoError(t, getErr)
			assert.Equal(t, tc.wantConflict, defaultEnvironment.Vars()["conflict"])
			if tc.wantAdded == "" {
				assert.NotContains(t, defaultEnvironment.Vars(), "added")
			} else {
				assert.Equal(t, tc.wantAdded, defaultEnvironment.Vars()["added"])
			}
			requests, listErr := st.ListRequests(context.Background(), target.ID)
			require.NoError(t, listErr)
			assert.Len(t, requests, tc.wantRequests)
		})
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func collectionPaths(t *testing.T, st *store.Store) map[string]*domain.Collection {
	t.Helper()
	collections, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	paths := make(map[string]*domain.Collection, len(collections))
	for _, collection := range collections {
		path, pathErr := st.CollectionPath(context.Background(), collection.ID)
		require.NoError(t, pathErr)
		paths[path] = collection
	}
	return paths
}
