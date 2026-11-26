package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func saveTestExecutions(t *testing.T, s *store.Store, requestID string, n int) []*domain.Execution {
	t.Helper()
	base := time.Now().Add(-time.Hour).UTC()
	out := make([]*domain.Execution, 0, n)
	for i := range n {
		ex := &domain.Execution{
			ID:              fmt.Sprintf("%s-ex-%02d", requestID, i),
			RequestID:       requestID,
			RequestSnapshot: `{"method":"GET","url":"https://example.com"}`,
			StatusCode:      200 + i,
			ResponseHeaders: `{"Content-Type":["text/plain"]}`,
			ResponseBody:    fmt.Sprintf("body-%d", i),
			StartedAt:       base.Add(time.Duration(i) * time.Second),
			CompletedAt:     base.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, s.SaveExecution(context.Background(), ex))
		out = append(out, ex)
	}
	return out
}

func TestListExecutionsByRequest_OmitsBodiesButKeepsSize(t *testing.T) {
	s := newTestStore(t)
	saveTestExecutions(t, s, "req-1", 2)

	got, err := s.ListExecutionsByRequest(context.Background(), "req-1", 0)
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, ex := range got {
		assert.True(t, ex.BodyOmitted)
		assert.Empty(t, ex.ResponseBody)
		assert.Equal(t, int64(len("body-0")), ex.BodySize())
	}
}

func TestListExecutionsByRequest_Limit(t *testing.T) {
	s := newTestStore(t)
	saved := saveTestExecutions(t, s, "req-1", 5)

	got, err := s.ListExecutionsByRequest(context.Background(), "req-1", 2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, saved[4].ID, got[0].ID, "newest first")
	assert.Equal(t, saved[3].ID, got[1].ID)

	all, err := s.ListExecutionsByRequest(context.Background(), "req-1", -1)
	require.NoError(t, err)
	assert.Len(t, all, 5)
}

func TestGetExecution_ReturnsBodyAndTruncation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ex := &domain.Execution{
		ID:                    "big",
		RequestID:             "req-1",
		RequestSnapshot:       `{}`,
		StatusCode:            200,
		ResponseBody:          "prefix",
		ResponseSize:          9000,
		ResponseBodyTruncated: true,
		StartedAt:             time.Now().UTC(),
		CompletedAt:           time.Now().UTC(),
	}
	require.NoError(t, s.SaveExecution(ctx, ex))

	got, err := s.GetExecution(ctx, "big")
	require.NoError(t, err)
	assert.False(t, got.BodyOmitted)
	assert.Equal(t, "prefix", got.ResponseBody)
	assert.Equal(t, int64(9000), got.ResponseSize)
	assert.True(t, got.ResponseBodyTruncated)
	assert.Equal(t, "{}", got.ResponseHeaders, "empty headers are stored as {}")
}

func TestMigrationV10BackfillsResponseSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quark.db")
	s, err := store.New(path)
	require.NoError(t, err)
	// Roll the database back to v9: drop the v10 columns and its version row.
	_, err = s.DB().Exec(`
ALTER TABLE executions DROP COLUMN response_size;
ALTER TABLE executions DROP COLUMN response_body_truncated;
DELETE FROM schema_versions WHERE version = 10;
INSERT INTO executions (id, request_id, request_snapshot, response_body, started_at, completed_at)
VALUES ('old', 'req-1', '{}', 'héllo', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');`)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	s, err = store.New(path)
	require.NoError(t, err)
	defer s.Close()
	got, err := s.GetExecution(context.Background(), "old")
	require.NoError(t, err)
	assert.Equal(t, int64(len("héllo")), got.ResponseSize, "size is in bytes")
	assert.False(t, got.ResponseBodyTruncated)
}

func TestGetExecution_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetExecution(context.Background(), "missing")
	assert.ErrorIs(t, err, store.ErrNotFound)
}
