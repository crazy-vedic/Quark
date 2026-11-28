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

func TestScratchRequests_ListedApartFromCollections(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	col := &domain.Collection{ID: "col", Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, col))

	scratch := &domain.Request{Name: "loose", Method: "GET", URL: "http://x"}
	filed := &domain.Request{CollectionID: col.ID, Name: "filed", Method: "GET", URL: "http://x"}
	require.NoError(t, s.SaveRequest(ctx, scratch))
	require.NoError(t, s.SaveRequest(ctx, filed))

	got, err := s.GetRequest(ctx, scratch.ID)
	require.NoError(t, err)
	assert.Empty(t, got.CollectionID)

	loose, err := s.ListRequests(ctx, "")
	require.NoError(t, err)
	require.Len(t, loose, 1)
	assert.Equal(t, "loose", loose[0].Name)

	inCol, err := s.ListRequests(ctx, col.ID)
	require.NoError(t, err)
	require.Len(t, inCol, 1)
	assert.Equal(t, "filed", inCol[0].Name)
}

func TestScratchRequests_NamesAreUniqueAmongScratch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dup := func() *domain.Request {
		return &domain.Request{Name: "dup", Method: "GET", URL: "http://x"}
	}
	require.NoError(t, s.SaveRequest(ctx, dup()))

	err := s.SaveRequest(ctx, dup())
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrDuplicate)
}

func TestScratchRequests_MoveIntoAndOutOfCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	col := &domain.Collection{ID: "col", Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, col))

	req := &domain.Request{Name: "mover", Method: "GET", URL: "http://x"}
	require.NoError(t, s.SaveRequest(ctx, req))

	req.CollectionID = col.ID
	require.NoError(t, s.SaveRequest(ctx, req))
	loose, err := s.ListRequests(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, loose)
	inCol, err := s.ListRequests(ctx, col.ID)
	require.NoError(t, err)
	require.Len(t, inCol, 1)

	req.CollectionID = ""
	require.NoError(t, s.SaveRequest(ctx, req))
	loose, err = s.ListRequests(ctx, "")
	require.NoError(t, err)
	require.Len(t, loose, 1)
	inCol, err = s.ListRequests(ctx, col.ID)
	require.NoError(t, err)
	assert.Empty(t, inCol)
}

func TestScratchRequests_NotAddressableByPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	col := &domain.Collection{ID: "col", Name: "API"}
	require.NoError(t, s.SaveCollection(ctx, col))
	require.NoError(t, s.SaveRequest(ctx, &domain.Request{
		CollectionID: col.ID, Name: "filed", Method: "GET", URL: "http://x",
	}))
	require.NoError(t, s.SaveRequest(ctx, &domain.Request{
		Name: "loose", Method: "GET", URL: "http://x",
	}))

	resolved, err := s.ResolveRequestPath(ctx, "API/filed")
	require.NoError(t, err, "a scratch request must not break path resolution")
	assert.Equal(t, "filed", resolved.Name)

	_, err = s.ResolveRequestPath(ctx, "loose")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// Databases created before v10 have requests.collection_id NOT NULL. Opening
// one must keep every request and its ID, and allow scratch requests after.
func TestMigration_ScratchRequestsRebuildKeepsRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(path)
	require.NoError(t, err)
	require.NoError(t, s.SaveCollection(ctx, &domain.Collection{ID: "col-1", Name: "Col"}))
	require.NoError(t, s.SaveRequest(ctx, &domain.Request{
		ID: "r1", CollectionID: "col-1", Name: "legacy", Method: "POST", URL: "http://x",
		Headers: `{"A":"1"}`, Body: "body", AuthType: "bearer", AuthConfig: `{"bearer":{}}`,
	}))
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `PRAGMA foreign_keys = OFF; PRAGMA legacy_alter_table = ON;
ALTER TABLE requests RENAME TO requests_v10;
DROP INDEX idx_requests_scratch_name;
CREATE TABLE requests (
    id TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL,
    name TEXT NOT NULL,
    method TEXT NOT NULL CHECK(method IN ('GET','POST','PUT','DELETE','PATCH','HEAD','OPTIONS')),
    url TEXT NOT NULL,
    headers TEXT DEFAULT '{}',
    body TEXT,
    sort_order INTEGER DEFAULT 0,
    enabled BOOLEAN DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    auth_type TEXT NOT NULL DEFAULT '',
    auth_config TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE,
    UNIQUE(collection_id, name)
);
INSERT INTO requests SELECT id, collection_id, name, method, url, headers, body, sort_order,
    enabled, created_at, updated_at, auth_type, auth_config FROM requests_v10;
DROP TABLE requests_v10;
DELETE FROM schema_versions WHERE version = 10;
PRAGMA legacy_alter_table = OFF; PRAGMA foreign_keys = ON;`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s, err = store.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	got, err := s.GetRequest(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "col-1", got.CollectionID)
	assert.Equal(t, "legacy", got.Name)
	assert.Equal(t, "body", got.Body)
	assert.Equal(t, "bearer", got.AuthType)

	require.NoError(t, s.SaveRequest(ctx, &domain.Request{
		Name: "after", Method: "GET", URL: "http://x",
	}))
	loose, err := s.ListRequests(ctx, "")
	require.NoError(t, err)
	require.Len(t, loose, 1)
}
