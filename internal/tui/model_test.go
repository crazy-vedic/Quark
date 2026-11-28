package tui

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

// The in-app scheduler must refuse a disabled request without sending it.
func TestExecuteDueScheduledRuns_RefusesDisabledRequest(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{ID: "col", Name: "API"}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		ID: "req", CollectionID: "col", Name: "off", Method: http.MethodGet,
		URL: "https://example.test/off", Disabled: true,
	}))
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID: "run", RequestID: "req", RunAt: now.Add(-time.Minute),
		Status: domain.ScheduledRunPending,
	}))

	sent := false
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		sent = true
		return &http.Response{
			StatusCode: 200, Status: "200 OK", Header: http.Header{},
			Body: io.NopCloser(http.NoBody),
		}, nil
	})
	msg := executeDueScheduledRunsCmd(
		ctx, st, st, st, exec.New(transport), st, st, nil, func() time.Time { return now },
	)()

	result, ok := msg.(scheduledRunBackgroundResultMsg)
	require.True(t, ok)
	require.Len(t, result.failed, 1)
	assert.ErrorIs(t, result.failed[0].err, domain.ErrRequestDisabled)
	assert.False(t, sent, "a disabled request must not be sent")

	run, err := st.GetScheduledRun(ctx, "run")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunFailed, run.Status)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
