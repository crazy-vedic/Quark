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

func TestSaveRequest_AcceptsAnyTokenMethod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c := &domain.Collection{ID: "col", Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	for _, method := range []string{"TRACE", "PROPFIND", "QUERY", "X-CUSTOM"} {
		req := &domain.Request{CollectionID: c.ID, Name: method, Method: method, URL: "http://x"}
		require.NoError(t, s.SaveRequest(ctx, req), method)
		got, err := s.GetRequest(ctx, req.ID)
		require.NoError(t, err)
		assert.Equal(t, method, got.Method)
	}
}

func TestSaveRequest_RejectsInvalidMethod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c := &domain.Collection{ID: "col", Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	for _, method := range []string{"", "GE T", "GET\n"} {
		req := &domain.Request{CollectionID: c.ID, Name: "bad", Method: method, URL: "http://x"}
		assert.ErrorIs(t, s.SaveRequest(ctx, req), domain.ErrInvalidMethod, "%q", method)
	}
}

func TestTransactionSaveRequest_AcceptsCustomAndRejectsInvalidMethod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.SaveCollection(ctx, &domain.Collection{ID: "col", Name: "Col"}))

	tx, err := s.BeginTransaction(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.SaveRequest(ctx,
		&domain.Request{CollectionID: "col", Name: "t", Method: "TRACE", URL: "http://x"}))
	assert.ErrorIs(
		t,
		tx.SaveRequest(
			ctx,
			&domain.Request{CollectionID: "col", Name: "b", Method: "B A D", URL: "http://x"},
		),
		domain.ErrInvalidMethod,
	)
	require.NoError(t, tx.Commit())
}

// legacyRequestsSchema rewinds a database to the requests table that v1..v9
// left behind, including the method CHECK constraint that v10 removes.
const legacyRequestsSchema = `
PRAGMA foreign_keys = OFF;
DROP TABLE requests;
CREATE TABLE requests (
    id            TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL,
    name          TEXT NOT NULL,
    method        TEXT NOT NULL CHECK(method IN ('GET','POST','PUT','DELETE','PATCH','HEAD','OPTIONS')),
    url           TEXT NOT NULL,
    headers       TEXT DEFAULT '{}',
    body          TEXT,
    sort_order    INTEGER DEFAULT 0,
    enabled       BOOLEAN DEFAULT 1,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    auth_type     TEXT NOT NULL DEFAULT '',
    auth_config   TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (collection_id) REFERENCES collections(id) ON DELETE CASCADE,
    UNIQUE(collection_id, name)
);
CREATE INDEX idx_requests_collection ON requests(collection_id, sort_order);
PRAGMA foreign_keys = ON;
DELETE FROM schema_versions WHERE version >= 10;
`

func TestMigration_DropsMethodCheckAndPreservesData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, legacyRequestsSchema)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO collections (id, name) VALUES ('col-1', 'Col');
INSERT INTO requests (id, collection_id, name, method, url, headers, body, sort_order, enabled, auth_type, auth_config)
VALUES ('req-1', 'col-1', 'List', 'POST', 'http://x', '{"A":"b"}', 'payload', 3, 0, 'bearer', '{"token":"t"}');
INSERT INTO scheduled_runs (id, request_id, run_at) VALUES ('run-1', 'req-1', '2030-01-01 00:00:00');`)
	require.NoError(t, err)
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO requests (id, collection_id, name, method, url) VALUES ('r2', 'col-1', 'T', 'TRACE', 'http://x')`,
	)
	require.Error(t, err, "legacy schema must reject TRACE")
	require.NoError(t, db.Close())

	s, err = store.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	got, err := s.GetRequest(ctx, "req-1")
	require.NoError(t, err)
	assert.Equal(t, "List", got.Name)
	assert.Equal(t, "POST", got.Method)
	assert.Equal(t, `{"A":"b"}`, got.Headers)
	assert.Equal(t, "payload", got.Body)
	assert.Equal(t, 3, got.SortOrder)
	assert.False(t, got.Enabled)
	assert.Equal(t, "bearer", got.AuthType)
	assert.Equal(t, `{"token":"t"}`, got.AuthConfig)

	trace := &domain.Request{CollectionID: "col-1", Name: "Trace", Method: "TRACE", URL: "http://x"}
	require.NoError(t, s.SaveRequest(ctx, trace))

	var ddl string
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='requests'`).Scan(&ddl))
	assert.NotContains(t, ddl, "CHECK")

	var idx int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_requests_collection'`).
		Scan(&idx))
	assert.Equal(t, 1, idx)
	dup := &domain.Request{CollectionID: "col-1", Name: "List", Method: "GET", URL: "http://x"}
	assert.ErrorIs(t, s.SaveRequest(ctx, dup), store.ErrDuplicate)

	var fkOn int
	require.NoError(t, s.DB().QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fkOn))
	assert.Equal(t, 1, fkOn)

	var runs int
	require.NoError(
		t,
		s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_runs`).Scan(&runs),
	)
	assert.Equal(t, 1, runs)
	require.NoError(t, s.DeleteRequest(ctx, "req-1"))
	require.NoError(
		t,
		s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_runs`).Scan(&runs),
	)
	assert.Equal(t, 0, runs, "scheduled_runs must still cascade from requests")

	// Deleting the collection still cascades to its requests.
	require.NoError(t, s.DeleteCollection(ctx, "col-1"))
	var reqs int
	require.NoError(t, s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM requests`).Scan(&reqs))
	assert.Equal(t, 0, reqs)
}
