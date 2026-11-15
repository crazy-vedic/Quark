package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// These tests reproduce known run-due defects. They are expected to FAIL on
// master until the corresponding fixes land.

// Issue 2: HTTP 5xx responses must not be recorded as completed runs.
func TestProof_RunDue_HTTP500IsNotMarkedCompleted(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID: "run-1", RequestID: "req-1", RunAt: now.Add(-time.Minute),
		Status: domain.ScheduledRunPending,
	}
	transport := &recordingRoundTripper{
		response: &http.Response{
			StatusCode: 500,
			Status:     "500 Internal Server Error",
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewBufferString("boom")),
		},
	}

	cmd := NewScheduleCmd(st, exec.New(transport), func() time.Time { return now })
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())

	assert.NotEqual(t, domain.ScheduledRunCompleted, st.runs["run-1"].Status,
		"a 500 response must not be recorded as a completed run")
	assert.Equal(t, domain.ScheduledRunFailed, st.runs["run-1"].Status)
}

// reentrantTransport calls onFirst during the first request, before that
// request returns. This models a second run-due invocation (cron plus manual)
// starting while the first is still executing.
type reentrantTransport struct {
	calls   int
	onFirst func()
}

func (rt *reentrantTransport) RoundTrip(*http.Request) (*http.Response, error) {
	rt.calls++
	if rt.calls == 1 && rt.onFirst != nil {
		rt.onFirst()
	}
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewBufferString("ok")),
	}, nil
}

// Issue 3: overlapping run-due invocations must not execute the same run twice.
func TestProof_RunDue_OverlappingInvocationsExecuteOnce(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID: "run-1", RequestID: "req-1", RunAt: now.Add(-time.Minute),
		Status: domain.ScheduledRunPending,
	}
	transport := &reentrantTransport{}
	executor := exec.New(transport)
	transport.onFirst = func() {
		inner := NewScheduleCmd(st, executor, func() time.Time { return now })
		inner.SetOut(new(bytes.Buffer))
		inner.SetArgs([]string{"run-due"})
		inner.SetContext(context.Background())
		_ = inner.Execute()
	}

	cmd := NewScheduleCmd(st, executor, func() time.Time { return now })
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())

	assert.Equal(t, 1, transport.calls,
		"the same scheduled run must hit the network exactly once")
}

// orderedScheduleStore makes due-run ordering deterministic for tests.
type orderedScheduleStore struct{ *fakeScheduleStore }

func (s orderedScheduleStore) ListDueScheduledRuns(
	ctx context.Context,
	now time.Time,
) ([]*domain.ScheduledRun, error) {
	runs, err := s.fakeScheduleStore.ListDueScheduledRuns(ctx, now)
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	return runs, err
}

// Issue 4: a failed status save after one run must not skip the rest of the batch.
func TestProof_RunDue_SaveFailureDoesNotAbortBatch(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()
	for _, id := range []string{"run-1", "run-2"} {
		st.runs[id] = &domain.ScheduledRun{
			ID: id, RequestID: "req-1", RunAt: now.Add(-time.Minute),
			Status: domain.ScheduledRunPending,
		}
	}
	st.saveErrByRunID = map[string]error{"run-1": assert.AnError}

	transport := &reentrantTransport{}
	cmd := NewScheduleCmd(orderedScheduleStore{st}, exec.New(transport), func() time.Time { return now })
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())
	_ = cmd.Execute()

	assert.Equal(t, 2, transport.calls,
		"run-2 must still be attempted when run-1's status save fails")
}
