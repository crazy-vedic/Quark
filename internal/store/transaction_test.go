package store

import (
	"context"
	"testing"

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
