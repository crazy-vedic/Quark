package store

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestTransaction_AllOrNothing(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	// Begin transaction
	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)

	// Save collection in transaction
	col := &domain.Collection{ID: "col-1", Name: "tx-test"}
	require.NoError(t, tx.SaveCollection(ctx, col))

	// Save request in transaction
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: col.ID,
		Name:         "test-req",
		Method:       "GET",
		URL:          "https://example.com",
		Headers:      "{}",
		AuthType:     domain.AuthTypeAPIKey,
		AuthConfig:   `{"in":"header","name":"X-API-Key","value":"secret"}`,
	}
	require.NoError(t, tx.SaveRequest(ctx, req))

	// Commit
	require.NoError(t, tx.Commit())

	// Verify collection exists
	cols, err := st.ListCollections(ctx)
	require.NoError(t, err)
	assert.Len(t, cols, 1)
	assert.Equal(t, "tx-test", cols[0].Name)

	// Verify request exists
	reqs, err := st.ListRequests(ctx, col.ID)
	require.NoError(t, err)
	assert.Len(t, reqs, 1)
	assert.Equal(t, "test-req", reqs[0].Name)
	assert.Equal(t, domain.AuthTypeAPIKey, reqs[0].AuthType)
}

func TestTransaction_Rollback(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	// Begin transaction
	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)

	// Save collection
	col := &domain.Collection{ID: "col-rollback", Name: "rollback-test"}
	require.NoError(t, tx.SaveCollection(ctx, col))

	// Rollback
	require.NoError(t, tx.Rollback())

	// Verify collection does NOT exist
	cols, err := st.ListCollections(ctx)
	require.NoError(t, err)
	assert.Empty(t, cols)
}

func TestTransaction_SaveEnvironment(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	// Create collection
	col := &domain.Collection{ID: "col-1", Name: "tx-env-test"}
	require.NoError(t, st.SaveCollection(ctx, col))

	// Begin transaction
	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)

	// Save environment in transaction
	env := &domain.Environment{
		ID:           "env-1",
		CollectionID: col.ID,
		Name:         "dev",
		Data:         `{"url": "http://localhost"}`,
	}
	require.NoError(t, tx.SaveEnvironment(ctx, env))

	// Commit
	require.NoError(t, tx.Commit())

	// Verify env exists
	got, err := st.GetEnvironment(ctx, env.ID)
	require.NoError(t, err)
	assert.Equal(t, "dev", got.Name)
	assert.Equal(t, "http://localhost", got.Vars()["url"])
}

func TestTransaction_SaveEnvironment_Duplicate(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: "col-1", Name: "tx-env-test"}
	require.NoError(t, st.SaveCollection(ctx, col))

	// Save env outside transaction
	env := &domain.Environment{
		ID:           "env-1",
		CollectionID: col.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	require.NoError(t, st.SaveEnvironment(ctx, env))

	// Try to save duplicate in transaction
	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)

	dupe := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "dev", // same name in same collection
		Data:         `{}`,
	}
	err = tx.SaveEnvironment(ctx, dupe)
	assert.ErrorIs(t, err, ErrDuplicate)
	require.NoError(t, tx.Rollback())
}

func TestTransaction_DeleteEnvironment(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: "col-1", Name: "tx-env-test"}
	require.NoError(t, st.SaveCollection(ctx, col))

	env := &domain.Environment{
		ID:           "env-1",
		CollectionID: col.ID,
		Name:         "dev",
		Data:         `{}`,
	}
	require.NoError(t, st.SaveEnvironment(ctx, env))

	// Delete in transaction
	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.DeleteEnvironment(ctx, env.ID))
	require.NoError(t, tx.Commit())

	// Verify deleted
	_, err = st.GetEnvironment(ctx, env.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestTransaction_DeleteEnvironment_NotFound(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)

	// Deleting non-existent env is not an error in SQLite DELETE
	err = tx.DeleteEnvironment(ctx, uuid.New().String())
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestTransaction_CreateDefaultEnv(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: "col-1", Name: "tx-env-test"}
	require.NoError(t, st.SaveCollection(ctx, col))

	// Already auto-created; should be idempotent
	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)

	env, err := tx.CreateDefaultEnvironment(ctx, col.ID)
	require.NoError(t, err)
	assert.Equal(t, "default", env.Name)
	assert.Equal(t, col.ID, env.CollectionID)
	require.NoError(t, tx.Commit())
}

func TestTransaction_EnvRollback(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	st, err := New(dbPath, WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: "col-1", Name: "tx-env-test"}
	require.NoError(t, st.SaveCollection(ctx, col))

	// Save env in transaction then rollback
	tx, err := st.BeginTransaction(ctx)
	require.NoError(t, err)

	env := &domain.Environment{
		ID:           "env-rollback",
		CollectionID: col.ID,
		Name:         "staging",
		Data:         `{}`,
	}
	require.NoError(t, tx.SaveEnvironment(ctx, env))
	require.NoError(t, tx.Rollback())

	// Verify env does NOT exist
	_, err = st.GetEnvironment(ctx, env.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}
