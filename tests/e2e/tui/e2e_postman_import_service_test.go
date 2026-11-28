//go:build e2e

package tui_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/postmanimport"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// storeBackedPostmanImporter runs the real Postman import service against a
// real store with the same options the production TUI adapter uses (the TUI
// never prompts, so name conflicts import a suffixed duplicate).
type storeBackedPostmanImporter struct {
	st *store.Store
}

func (i storeBackedPostmanImporter) ImportPostman(
	ctx context.Context,
	path string,
) (postmanimport.Result, error) {
	return postmanimport.New(i.st, nil).Import(ctx, path, postmanimport.Options{
		OnDuplicate: postmanimport.ActionDuplicate,
	})
}

func openStoreBackedPostmanImport(t *testing.T, st *store.Store) tui.Model {
	t.Helper()
	cfg := config.Default(t.TempDir())
	m := tui.New(tui.Deps{
		PostmanImporter: storeBackedPostmanImporter{st: st},
		Config:          cfg,
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
		Ctx:             context.Background(),
	}).WithFocus(tui.SidebarPane)
	m = resize(t, m, 120, 40)
	return callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
}

// submitPostmanImportPath types path into the import modal, presses Enter and
// delivers the asynchronous import result back to the model.
func submitPostmanImportPath(t *testing.T, m tui.Model, path string) tui.Model {
	t.Helper()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(path)})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	return executeCmdUpdate(t, m, cmd)
}

func writeRealPostmanFile(t *testing.T, path string, value any) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	data, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func realPostmanCollection(name string, items ...map[string]any) map[string]any {
	return map[string]any{
		"info": map[string]any{
			"name":   name,
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		"variable": []map[string]any{{"key": "base_url", "value": "https://example.test"}},
		"item":     items,
	}
}

func realPostmanRequest(name, rawURL string, headers ...map[string]any) map[string]any {
	request := map[string]any{
		"method": "GET",
		"url":    map[string]any{"raw": rawURL},
	}
	if len(headers) > 0 {
		request["header"] = headers
	}
	return map[string]any{"name": name, "request": request}
}

func TestE2E_PostmanImportService_SingleFileCreatesThenDuplicates(t *testing.T) {
	st := setupStore(t)
	file := filepath.Join(t.TempDir(), "collection.json")
	writeRealPostmanFile(t, file, realPostmanCollection("Real API",
		realPostmanRequest("Ping", "{{base_url}}/ping"),
		map[string]any{
			"name": "Users",
			"item": []map[string]any{
				realPostmanRequest("List Users", "{{base_url}}/users"),
			},
		},
	))

	m := openStoreBackedPostmanImport(t, st)
	m = submitPostmanImportPath(t, m, file)
	assertViewContains(t, m, `Imported 2/2 requests into "Real API"`)

	roots := rootCollections(t, st)
	require.Len(t, roots, 1)
	assert.Equal(t, "Real API", roots[0].Name)

	// A second import of the same file keeps the first root and adds a suffixed
	// duplicate, which the modal reports as an outcome.
	m = openStoreBackedPostmanImport(t, st)
	m = submitPostmanImportPath(t, m, file)
	assertViewContains(t, m, `Imported 2/2 requests into "Real API 1" (duplicated)`)

	assert.Len(t, rootCollections(t, st), 2)
}

func TestE2E_PostmanImportService_CredentialHeaderIsFlagged(t *testing.T) {
	st := setupStore(t)
	file := filepath.Join(t.TempDir(), "secured.json")
	writeRealPostmanFile(t, file, realPostmanCollection("Secured API",
		realPostmanRequest("Admin", "https://example.test/admin", map[string]any{
			"key":   "Authorization",
			"value": "Basic dXNlcjpzZWNyZXQtcGFzc3dvcmQ=",
		}),
	))

	m := openStoreBackedPostmanImport(t, st)
	m = submitPostmanImportPath(t, m, file)
	assertViewContains(t, m, `Imported 1/1 requests into "Secured API" · may contain credentials`)
}

func TestE2E_PostmanImportService_InvalidFileShowsFailure(t *testing.T) {
	st := setupStore(t)
	file := filepath.Join(t.TempDir(), "broken.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"info":`), 0o600))

	m := openStoreBackedPostmanImport(t, st)
	m = submitPostmanImportPath(t, m, file)
	assertViewContains(t, m, "Import failed: ")

	assert.Empty(t, rootCollections(t, st), "a file that does not parse must not write a collection")
}

func TestE2E_PostmanImportService_MissingPathShowsFailure(t *testing.T) {
	st := setupStore(t)
	// The modal hard-wraps long paths mid-word, so the test uses a short
	// relative name in an empty directory. The file cannot exist there, and
	// the error line fits on one row.
	t.Chdir(t.TempDir())
	const missing = "does-not-exist.json"
	_, statErr := os.Stat(missing)
	require.ErrorIs(t, statErr, fs.ErrNotExist)

	m := openStoreBackedPostmanImport(t, st)
	m = submitPostmanImportPath(t, m, missing)
	assertViewContains(t, m, "Import failed: ")
	assertViewContains(t, m, missing)
	// The OS wording differs by platform. Windows wraps its message, so only
	// its last words are checked there.
	if runtime.GOOS == "windows" {
		assertViewContains(t, m, "find the file specified")
	} else {
		assertViewContains(t, m, "no such file or directory")
	}
}

func TestE2E_PostmanImportService_ExportDirectorySummarizesCollectionsAndEnvironments(t *testing.T) {
	st := setupStore(t)
	exportDir := filepath.Join(t.TempDir(), "postman-export")
	writeRealPostmanFile(t, filepath.Join(exportDir, "collection", "alpha.json"),
		realPostmanCollection("Alpha", realPostmanRequest("One", "https://example.test/one")))
	writeRealPostmanFile(t, filepath.Join(exportDir, "collection", "beta.json"),
		realPostmanCollection("Beta",
			realPostmanRequest("Two", "https://example.test/two"),
			realPostmanRequest("Three", "https://example.test/three"),
		))
	writeRealPostmanFile(t, filepath.Join(exportDir, "environment", "dev.json"), map[string]any{
		"id": "dev", "name": "Dev", "scope": "environment",
		"values": []map[string]any{
			{"key": "host", "value": "dev.example.test", "enabled": true},
		},
	})
	writeRealPostmanFile(t, filepath.Join(exportDir, "environment", "globals.json"), map[string]any{
		"id": "globals", "name": "Globals", "scope": "global",
		"values": []map[string]any{
			{"key": "region", "value": "eu", "enabled": true},
		},
	})
	require.NoError(t, os.WriteFile(
		filepath.Join(exportDir, "environment", "broken.json"), []byte(`{"name":`), 0o600))

	m := openStoreBackedPostmanImport(t, st)
	m = submitPostmanImportPath(t, m, exportDir)
	assertViewContains(t, m, "requests imported across 2 collections")
	assertViewContains(t, m, `Imported 1/1 requests into "Alpha"`)
	assertViewContains(t, m, `Imported 2/2 requests into "Beta"`)
	assertViewContains(t, m, "Environments: 3 imported, 1 errors")
	assertViewContains(t, m, "broken.json")
	assert.Len(t, rootCollections(t, st), 2)
}

func TestE2E_PostmanImportService_EmptyDirectoryShowsFailure(t *testing.T) {
	st := setupStore(t)
	empty := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(empty, "collection"), 0o700))

	m := openStoreBackedPostmanImport(t, st)
	m = submitPostmanImportPath(t, m, empty)
	assertViewContains(t, m, "Import failed: no collection files found")
	assert.Empty(t, rootCollections(t, st))
}

func TestE2E_PostmanImportModal_EmptyResultReportsNothingImported(t *testing.T) {
	importer := &postmanImportFake{result: postmanimport.Result{}}
	cfg := config.Default(t.TempDir())
	m := tui.New(tui.Deps{
		PostmanImporter: importer,
		Config:          cfg,
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
		Ctx:             context.Background(),
	}).WithFocus(tui.SidebarPane)
	m = resize(t, m, 120, 40)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = submitPostmanImportPath(t, m, "collection.json")
	assertViewContains(t, m, "Nothing was imported.")
}

// rootCollections returns the collections that are not nested under another
// collection; Postman folders are imported as child collections.
func rootCollections(t *testing.T, st *store.Store) []*domain.Collection {
	t.Helper()
	cols, err := st.ListCollections(context.Background())
	require.NoError(t, err)
	var roots []*domain.Collection
	for _, col := range cols {
		if col.ParentID == "" {
			roots = append(roots, col)
		}
	}
	return roots
}
