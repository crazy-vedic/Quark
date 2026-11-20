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

func TestStore_ClaimScheduledRun_OnlyOneClaimWins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := seedScheduledRunRequest(t, s)
	run := &domain.ScheduledRun{
		ID: uuid.New().String(), RequestID: req.ID, RunAt: time.Now().Add(-time.Minute),
	}
	require.NoError(t, s.SaveScheduledRun(ctx, run))

	first, err := s.ClaimScheduledRun(ctx, run.ID)
	require.NoError(t, err)
	assert.True(t, first)

	second, err := s.ClaimScheduledRun(ctx, run.ID)
	require.NoError(t, err)
	assert.False(t, second, "a running run cannot be claimed again")

	got, err := s.GetScheduledRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunRunning, got.Status)

	missing, err := s.ClaimScheduledRun(ctx, "no-such-run")
	require.NoError(t, err)
	assert.False(t, missing)
}

// A run claimed by a process that then died stays "running" forever. Runs that
// have been running since before the cutoff must be recovered as failed, and
// recent ones left alone.
func TestStore_RecoverStaleScheduledRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := seedScheduledRunRequest(t, s)

	for _, run := range []struct {
		id     string
		status string
	}{
		{"stale", domain.ScheduledRunRunning},
		{"fresh", domain.ScheduledRunRunning},
		{"pending", domain.ScheduledRunPending},
	} {
		require.NoError(t, s.SaveScheduledRun(ctx, &domain.ScheduledRun{
			ID: run.id, RequestID: req.ID, RunAt: time.Now().Add(-time.Hour),
			Status: run.status,
		}))
	}
	// Make "stale" look like it was claimed two hours ago.
	_, err := s.DB().ExecContext(ctx,
		`UPDATE scheduled_runs SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).UTC().Format("2006-01-02 15:04:05"), "stale")
	require.NoError(t, err)

	n, err := s.RecoverStaleScheduledRuns(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	stale, err := s.GetScheduledRun(ctx, "stale")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunFailed, stale.Status)
	assert.NotEmpty(t, stale.LastError, "the recovery must say why")

	fresh, err := s.GetScheduledRun(ctx, "fresh")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunRunning, fresh.Status)

	pending, err := s.GetScheduledRun(ctx, "pending")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunPending, pending.Status)
}

// Due detection must compare instants, not the text of zoned timestamps.
func TestStore_ListDueScheduledRuns_ComparesInstantsAcrossZones(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := seedScheduledRunRequest(t, s)
	india := time.FixedZone("IST", 5*3600+30*60)

	// Scheduled for 10:00 UTC.
	run := &domain.ScheduledRun{
		ID:        uuid.New().String(),
		RequestID: req.ID,
		RunAt:     time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC),
	}
	require.NoError(t, s.SaveScheduledRun(ctx, run))

	// 12:00 +05:30 is 06:30 UTC: three and a half hours before the run.
	early := time.Date(2026, 11, 17, 12, 0, 0, 0, india)
	due, err := s.ListDueScheduledRuns(ctx, early)
	require.NoError(t, err)
	assert.Empty(t, due, "run must not be due before its instant")

	// 16:00 +05:30 is 10:30 UTC: after the run.
	late := time.Date(2026, 11, 17, 16, 0, 0, 0, india)
	due, err = s.ListDueScheduledRuns(ctx, late)
	require.NoError(t, err)
	require.Len(t, due, 1)

	// A run saved with a zone offset must be ordered by instant as well.
	zoned := &domain.ScheduledRun{
		ID:        uuid.New().String(),
		RequestID: req.ID,
		RunAt:     time.Date(2026, 11, 17, 14, 0, 0, 0, india), // 08:30 UTC
	}
	require.NoError(t, s.SaveScheduledRun(ctx, zoned))
	next, err := s.NextPendingScheduledRun(ctx)
	require.NoError(t, err)
	assert.Equal(t, zoned.ID, next.ID, "08:30 UTC run comes before the 10:00 UTC run")
}
