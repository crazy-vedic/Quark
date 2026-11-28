package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestSaveRequest_RoundTripsDisabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	col := &domain.Collection{ID: "col", Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, col))

	on := &domain.Request{CollectionID: col.ID, Name: "on", Method: "GET", URL: "http://x"}
	off := &domain.Request{
		CollectionID: col.ID, Name: "off", Method: "GET", URL: "http://x", Disabled: true,
	}
	require.NoError(t, s.SaveRequest(ctx, on))
	require.NoError(t, s.SaveRequest(ctx, off))

	got, err := s.GetRequest(ctx, on.ID)
	require.NoError(t, err)
	assert.False(t, got.Disabled, "a new request is enabled by default")
	got, err = s.GetRequest(ctx, off.ID)
	require.NoError(t, err)
	assert.True(t, got.Disabled)

	list, err := s.ListRequests(ctx, col.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	byName := map[string]bool{list[0].Name: list[0].Disabled, list[1].Name: list[1].Disabled}
	assert.Equal(t, map[string]bool{"on": false, "off": true}, byName)

	resolved, err := s.ResolveRequestPath(ctx, "Col/off")
	require.NoError(t, err)
	assert.True(t, resolved.Disabled)

	// Re-enabling persists too.
	off.Disabled = false
	require.NoError(t, s.SaveRequest(ctx, off))
	got, err = s.GetRequest(ctx, off.ID)
	require.NoError(t, err)
	assert.False(t, got.Disabled)
}

// Before migration v10 every request Quark saved was written with enabled = 0
// even though nobody disabled it. Opening such a database must not turn every
// saved request into a disabled one.
func TestMigration_ReenablesLegacyRequests(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM schema_versions WHERE version >= 10`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO collections (id, name) VALUES ('col-1', 'Col')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO requests (id, collection_id, name, method, url, enabled)
		 VALUES ('r1', 'col-1', 'legacy', 'GET', 'http://x', 0)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s, err = store.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	got, err := s.GetRequest(ctx, "r1")
	require.NoError(t, err)
	assert.False(t, got.Disabled)
}
