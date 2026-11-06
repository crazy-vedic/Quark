package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// --- Schema and WAL ---

func TestStore_Open_CreatesSchema(t *testing.T) {
	s := newTestStore(t)

	// tables must exist
	for _, table := range []string{
		"collections",
		"requests",
		"environments",
		"executions",
		"schema_versions",
	} {
		var name string
		row := s.DB().
			QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table)
		err := row.Scan(&name)
		require.NoError(t, err, "table %q must exist", table)
		assert.Equal(t, table, name)
	}
}

func TestStore_WALMode(t *testing.T) {
	s := newTestStore(t)

	var mode string
	row := s.DB().QueryRow("PRAGMA journal_mode")
	require.NoError(t, row.Scan(&mode))
	assert.Equal(t, "wal", mode)
}

func TestStore_BusyTimeout(t *testing.T) {
	s := newTestStore(t)

	var timeout int
	row := s.DB().QueryRow("PRAGMA busy_timeout")
	require.NoError(t, row.Scan(&timeout))
	assert.Equal(t, 5000, timeout)
}

// --- WithCacheSize option ---

func TestWithCacheSize_Zero_Rejected(t *testing.T) {
	_, err := store.New(filepath.Join(t.TempDir(), "test.db"), store.WithCacheSize(0))
	require.Error(t, err, "WithCacheSize(0) must be rejected")
}

func TestWithCacheSize_Comparable(t *testing.T) {
	assert.Equal(t, store.WithCacheSize(64), store.WithCacheSize(64))
}

// --- Collection CRUD ---

func TestStore_SaveCollection_And_GetCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Users"}
	require.NoError(t, s.SaveCollection(ctx, c))

	got, err := s.GetCollection(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c.ID, got.ID)
	assert.Equal(t, c.Name, got.Name)
}

func TestStore_SaveCollection_DuplicateName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c1 := &domain.Collection{ID: uuid.New().String(), Name: "Users"}
	c2 := &domain.Collection{ID: uuid.New().String(), Name: "Users"}
	require.NoError(t, s.SaveCollection(ctx, c1))

	err := s.SaveCollection(ctx, c2)
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrDuplicate)
}

func TestStore_GetCollection_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetCollection(ctx, uuid.New().String())
	assert.Nil(t, got)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestStore_DeleteCollection_Cascades(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Payments"}
	require.NoError(t, s.SaveCollection(ctx, c))

	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "List",
		Method:       "GET",
		URL:          "http://example.com/payments",
	}
	require.NoError(t, s.SaveRequest(ctx, req))

	require.NoError(t, s.DeleteCollection(ctx, c.ID))

	// request must also be gone
	got, err := s.GetRequest(ctx, req.ID)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// --- ListCollections ordering ---

func TestStore_ListCollections_AlphabeticalOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"Users", "Payments", "Auth"} {
		err := s.SaveCollection(ctx, &domain.Collection{ID: uuid.New().String(), Name: name})
		require.NoError(t, err)
	}

	cols, err := s.ListCollections(ctx)
	require.NoError(t, err)
	require.Len(t, cols, 3)
	assert.Equal(t, "Auth", cols[0].Name)
	assert.Equal(t, "Payments", cols[1].Name)
	assert.Equal(t, "Users", cols[2].Name)
}

func TestStore_ListCollections_EmptyReturnsNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cols, err := s.ListCollections(ctx)
	require.NoError(t, err)
	assert.Nil(t, cols, "empty store must return nil, not empty slice")
}

// --- Request CRUD ---

func TestStore_SaveRequest_And_GetRequest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: c.ID,
		Name:         "List Users",
		Method:       "GET",
		URL:          "http://example.com/users",
		AuthType:     domain.AuthTypeBearer,
		AuthConfig:   `{"token":"secret-token"}`,
	}
	require.NoError(t, s.SaveRequest(ctx, req))

	got, err := s.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, req.ID, got.ID)
	assert.Equal(t, req.Name, got.Name)
	assert.Equal(t, req.AuthType, got.AuthType)
	assert.Equal(t, req.AuthConfig, got.AuthConfig)
}

// TestStore_SaveRequest_GeneratesIDWhenEmpty guards against the bug where two
// new requests saved with an empty ID collided on the same primary key ("") and
// the second overwrote the first via ON CONFLICT(id) DO UPDATE.
func TestStore_SaveRequest_GeneratesIDWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	req1 := &domain.Request{CollectionID: c.ID, Name: "new request", Method: "GET"}
	require.NoError(t, s.SaveRequest(ctx, req1))
	assert.NotEmpty(t, req1.ID, "SaveRequest should generate an ID when empty")

	req2 := &domain.Request{CollectionID: c.ID, Name: "new request 2", Method: "GET"}
	require.NoError(t, s.SaveRequest(ctx, req2))
	assert.NotEmpty(t, req2.ID)
	assert.NotEqual(t, req1.ID, req2.ID, "each new request must get a distinct ID")

	reqs, err := s.ListRequests(ctx, c.ID)
	require.NoError(t, err)
	assert.Len(t, reqs, 2, "both requests should persist without overwriting")
}

func TestStore_MigrationRepairsLegacyEmptyRequestIDs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `DELETE FROM schema_versions WHERE version = 7`)
	require.NoError(t, err)
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO collections (id, name) VALUES ('col-1', 'Col')`,
	)
	require.NoError(t, err)
	_, err = db.ExecContext(
		ctx,
		`INSERT INTO requests (id, collection_id, name, method, url, headers, auth_config)
		 VALUES ('', 'col-1', 'ewq', 'GET', '', '{}', '{}')`,
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	s, err = store.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	reqs, err := s.ListRequests(ctx, "col-1")
	require.NoError(t, err)
	require.Len(t, reqs, 1)
	assert.NotEmpty(t, reqs[0].ID)
	assert.NotEqual(t, "", reqs[0].ID)
}

func TestStore_GetRequest_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetRequest(ctx, uuid.New().String())
	assert.Nil(t, got)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// --- ListRequests ordering ---

func TestStore_ListRequests_OrderBySortOrderThenCreatedAtThenID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Col"}
	require.NoError(t, s.SaveCollection(ctx, c))

	// All share sort_order=0; tiebreaker is created_at ASC, then id ASC.
	// We insert with predictable IDs so the id tiebreaker is testable.
	ids := []string{"ccc-req", "aaa-req", "bbb-req"}
	for _, id := range ids {
		req := &domain.Request{
			ID:           id,
			CollectionID: c.ID,
			Name:         id,
			Method:       "GET",
			URL:          "http://example.com",
			SortOrder:    0,
		}
		require.NoError(t, s.SaveRequest(ctx, req))
	}

	reqs, err := s.ListRequests(ctx, c.ID)
	require.NoError(t, err)
	require.Len(t, reqs, 3)
	// created_at will be identical (same millisecond in test); id ASC tiebreaker applies
	assert.Equal(t, "aaa-req", reqs[0].ID)
	assert.Equal(t, "bbb-req", reqs[1].ID)
	assert.Equal(t, "ccc-req", reqs[2].ID)
}

func TestStore_ListRequests_EmptyCollectionReturnsNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &domain.Collection{ID: uuid.New().String(), Name: "Empty"}
	require.NoError(t, s.SaveCollection(ctx, c))

	reqs, err := s.ListRequests(ctx, c.ID)
	require.NoError(t, err)
	assert.Nil(t, reqs, "empty collection must return nil, not empty slice")
}

func TestStore_ListRequests_NonExistentCollection_ErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	reqs, err := s.ListRequests(ctx, uuid.New().String())
	assert.Nil(t, reqs)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestStore_SaveExecution_And_ListExecutionsByRequest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	exOlder := &domain.Execution{
		ID:              uuid.New().String(),
		RequestID:       "req-1",
		RequestSnapshot: `{"method":"GET","url":"https://example.com"}`,
		StatusCode:      200,
		ResponseHeaders: `{"Content-Type":["application/json"]}`,
		ResponseBody:    `{"ok":true}`,
		ResponseTimeMs:  12,
		StartedAt:       time.Now().Add(-2 * time.Minute).UTC(),
		CompletedAt:     time.Now().Add(-2 * time.Minute).UTC(),
	}
	exNewer := &domain.Execution{
		ID:              uuid.New().String(),
		RequestID:       "req-1",
		RequestSnapshot: `{"method":"GET","url":"https://example.com"}`,
		StatusCode:      500,
		ResponseHeaders: `{"Content-Type":["application/json"]}`,
		ResponseBody:    `{"error":"boom"}`,
		ResponseTimeMs:  34,
		StartedAt:       time.Now().Add(-1 * time.Minute).UTC(),
		CompletedAt:     time.Now().Add(-1 * time.Minute).UTC(),
		Error:           "server exploded",
	}
	require.NoError(t, s.SaveExecution(ctx, exOlder))
	require.NoError(t, s.SaveExecution(ctx, exNewer))

	got, err := s.ListExecutionsByRequest(ctx, "req-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, exNewer.ID, got[0].ID)
	assert.Equal(t, exOlder.ID, got[1].ID)
	assert.Equal(t, "server exploded", got[0].Error)
}

func TestStore_ListExecutionsByRequest_EmptyReturnsNil(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListExecutionsByRequest(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// --- UUID round-trip ---

func TestStore_UUIDPKsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id := uuid.New().String()
	c := &domain.Collection{ID: id, Name: "RoundTrip"}
	require.NoError(t, s.SaveCollection(ctx, c))

	cols, err := s.ListCollections(ctx)
	require.NoError(t, err)
	require.Len(t, cols, 1)
	assert.Equal(t, id, cols[0].ID)
}

// --- Compile-time interface compliance (static check, always passes) ---

func TestStore_CompileTimeInterfaceChecks(t *testing.T) {
	// These are compile-time checks in store.go. If they broke, this file
	// would not compile. This test documents that they exist.
	t.Log("compile-time interface checks are in internal/store/store.go")
}
