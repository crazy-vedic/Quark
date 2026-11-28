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

func newHistoryStore(t *testing.T, opts ...store.Option) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "quark.db"), opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func saveRuns(t *testing.T, s *store.Store, requestID string, n int) []string {
	t.Helper()
	base := time.Now().Add(-time.Hour).UTC()
	ids := make([]string, 0, n)
	for i := range n {
		ex := &domain.Execution{
			ID:              fmt.Sprintf("%s-%02d", requestID, i),
			RequestID:       requestID,
			RequestSnapshot: `{}`,
			StatusCode:      200,
			StartedAt:       base.Add(time.Duration(i) * time.Second),
			CompletedAt:     base.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, s.SaveExecution(context.Background(), ex))
		ids = append(ids, ex.ID)
	}
	return ids
}

func listIDs(t *testing.T, s *store.Store, requestID string) []string {
	t.Helper()
	got, err := s.ListExecutionsByRequest(context.Background(), requestID)
	require.NoError(t, err)
	ids := make([]string, 0, len(got))
	for _, ex := range got {
		ids = append(ids, ex.ID)
	}
	return ids
}

func TestSaveExecution_PrunesToKeepLastPerRequest(t *testing.T) {
	s := newHistoryStore(t, store.WithHistoryKeepLast(3))
	ids := saveRuns(t, s, "req-a", 5)
	other := saveRuns(t, s, "req-b", 2)

	assert.Equal(t, []string{ids[4], ids[3], ids[2]}, listIDs(t, s, "req-a"),
		"only the newest three runs are kept")
	assert.Equal(t, []string{other[1], other[0]}, listIDs(t, s, "req-b"),
		"pruning one request leaves other requests alone")
}

func TestSaveExecution_DefaultKeepsEverything(t *testing.T) {
	s := newHistoryStore(t)
	saveRuns(t, s, "req-a", 6)
	assert.Len(t, listIDs(t, s, "req-a"), 6)
}

func TestSaveExecution_ZeroKeepLastKeepsEverything(t *testing.T) {
	s := newHistoryStore(t, store.WithHistoryKeepLast(0))
	saveRuns(t, s, "req-a", 4)
	assert.Len(t, listIDs(t, s, "req-a"), 4)
}

func TestSaveExecution_PruneRemovesOlderBacklog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quark.db")
	s, err := store.New(path)
	require.NoError(t, err)
	saveRuns(t, s, "req-a", 5)
	require.NoError(t, s.Close())

	// Lowering keep_last trims the backlog on the next run.
	s, err = store.New(path, store.WithHistoryKeepLast(2))
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.SaveExecution(context.Background(), &domain.Execution{
		ID:              "req-a-new",
		RequestID:       "req-a",
		RequestSnapshot: `{}`,
		StartedAt:       time.Now().UTC(),
		CompletedAt:     time.Now().UTC(),
	}))
	assert.Equal(t, []string{"req-a-new", "req-a-04"}, listIDs(t, s, "req-a"))
}
