//go:build e2e

package cli

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// A collection whose requests cannot be read must not vanish silently from
// search: hits from the other collections are printed, the skipped collection is
// named on stderr, and the exit status tells scripts the result is partial.
func TestCLI_SearchReportsCollectionThatCouldNotBeSearched(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Healthy", Method: "GET", URL: "https://example.test/ok",
	})

	dbPath := filepath.Join(home, ".quark", "quark.db")
	brokenID := uuid.New().String()
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	require.NoError(t, st.SaveCollection(context.Background(),
		&domain.Collection{ID: brokenID, Name: "Broken"}))
	require.NoError(t, st.Close())
	seedRequest(t, home, brokenID, &domain.Request{
		Name: "Unreadable", Method: "GET", URL: "https://example.test/bad",
	})

	// Make the row unscannable: sort_order is an integer column.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(
		`UPDATE requests SET sort_order = 'not-a-number' WHERE collection_id = ?`,
		brokenID,
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	out, errOut, code := runQuarkWithHome(t, home, "search", "")
	assert.NotEqual(t, 0, code, "a partial search must exit non-zero")
	assert.Contains(t, out, "Healthy", "hits from the readable collection are still printed")
	assert.NotContains(t, out, "Unreadable")
	assert.Contains(t, errOut, "search: skipped Broken:")
	assert.Contains(t, errOut, "1 collection(s) could not be searched")
}

// When every collection can be read, search is unchanged and exits zero.
func TestCLI_SearchWithNoFailuresExitsZero(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Only", Method: "GET", URL: "https://example.test/only",
	})
	out, errOut, code := runQuarkWithHome(t, home, "search", "only")
	require.Equal(t, 0, code, "search failed: %s", errOut)
	assert.Contains(t, out, "Only")
	assert.NotContains(t, errOut, "skipped")
}
