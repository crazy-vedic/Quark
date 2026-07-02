package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestStore_SaveListAndGetScheduledRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := seedScheduledRunRequest(t, s)
	runAt := time.Date(2026, 6, 25, 15, 30, 0, 0, time.UTC)

	run := &domain.ScheduledRun{
		ID:        uuid.New().String(),
		RequestID: req.ID,
		RunAt:     runAt,
		Status:    domain.ScheduledRunPending,
	}
	require.NoError(t, s.SaveScheduledRun(ctx, run))

	got, err := s.GetScheduledRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, req.ID, got.RequestID)
	assert.Equal(t, domain.ScheduledRunPending, got.Status)
	assert.Equal(t, runAt.Unix(), got.RunAt.Unix())

	all, err := s.ListScheduledRuns(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, run.ID, all[0].ID)
}

func TestStore_ListDueScheduledRuns_OnlyPendingAtOrBeforeNow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := seedScheduledRunRequest(t, s)
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)

	runs := []*domain.ScheduledRun{
		{
			ID:        "due",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Minute),
			Status:    domain.ScheduledRunPending,
		},
		{
			ID:        "future",
			RequestID: req.ID,
			RunAt:     now.Add(time.Minute),
			Status:    domain.ScheduledRunPending,
		},
		{
			ID:        "done",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Hour),
			Status:    domain.ScheduledRunCompleted,
		},
	}
	for _, run := range runs {
		require.NoError(t, s.SaveScheduledRun(ctx, run))
	}

	due, err := s.ListDueScheduledRuns(ctx, now)
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, "due", due[0].ID)
}

func TestStore_NextPendingScheduledRun_ReturnsEarliestPending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := seedScheduledRunRequest(t, s)
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)

	runs := []*domain.ScheduledRun{
		{
			ID:        "future-2",
			RequestID: req.ID,
			RunAt:     now.Add(2 * time.Hour),
			Status:    domain.ScheduledRunPending,
		},
		{
			ID:        "done",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Hour),
			Status:    domain.ScheduledRunCompleted,
		},
		{
			ID:        "future-1",
			RequestID: req.ID,
			RunAt:     now.Add(time.Hour),
			Status:    domain.ScheduledRunPending,
		},
	}
	for _, run := range runs {
		require.NoError(t, s.SaveScheduledRun(ctx, run))
	}

	next, err := s.NextPendingScheduledRun(ctx)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, "future-1", next.ID)
}

func seedScheduledRunRequest(t *testing.T, s *store.Store) *domain.Request {
	t.Helper()
	ctx := context.Background()
	col := &domain.Collection{ID: uuid.New().String(), Name: "Scheduled"}
	require.NoError(t, s.SaveCollection(ctx, col))
	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "Ping",
		Method:       "GET",
		URL:          "https://example.test/ping",
	}
	require.NoError(t, s.SaveRequest(ctx, req))
	return req
}
