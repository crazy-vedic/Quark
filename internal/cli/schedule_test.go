package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

type fakeScheduleStore struct {
	fakeRunStore
	runs              map[string]*domain.ScheduledRun
	saveErrByRunID    map[string]error
	saveCallCountByID map[string]int
}

func (s *fakeScheduleStore) SaveScheduledRun(_ context.Context, run *domain.ScheduledRun) error {
	if s.saveCallCountByID == nil {
		s.saveCallCountByID = make(map[string]int)
	}
	s.saveCallCountByID[run.ID]++
	if err := s.saveErrByRunID[run.ID]; err != nil {
		return err
	}
	if s.runs == nil {
		s.runs = make(map[string]*domain.ScheduledRun)
	}
	cloned := *run
	s.runs[run.ID] = &cloned
	return nil
}

// ClaimScheduledRun mirrors the store: only a pending run can be claimed.
func (s *fakeScheduleStore) ClaimScheduledRun(_ context.Context, id string) (bool, error) {
	run, ok := s.runs[id]
	if !ok || run.Status != domain.ScheduledRunPending {
		return false, nil
	}
	claimed := *run
	claimed.Status = domain.ScheduledRunRunning
	claimed.LastError = ""
	s.runs[id] = &claimed
	return true, nil
}

func (s *fakeScheduleStore) GetScheduledRun(
	_ context.Context,
	id string,
) (*domain.ScheduledRun, error) {
	return s.runs[id], nil
}

func (s *fakeScheduleStore) GetRequest(ctx context.Context, id string) (*domain.Request, error) {
	req, err := s.fakeRunStore.GetRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, assert.AnError
	}
	return req, nil
}

func (s *fakeScheduleStore) ListScheduledRuns(context.Context) ([]*domain.ScheduledRun, error) {
	var out []*domain.ScheduledRun
	for _, run := range s.runs {
		out = append(out, run)
	}
	return out, nil
}

func (s *fakeScheduleStore) ListDueScheduledRuns(
	_ context.Context,
	now time.Time,
) ([]*domain.ScheduledRun, error) {
	var out []*domain.ScheduledRun
	for _, run := range s.runs {
		if run.Status == domain.ScheduledRunPending && !run.RunAt.After(now) {
			out = append(out, run)
		}
	}
	return out, nil
}

func (s *fakeScheduleStore) NextPendingScheduledRun(context.Context) (*domain.ScheduledRun, error) {
	var next *domain.ScheduledRun
	for _, run := range s.runs {
		if run.Status != domain.ScheduledRunPending {
			continue
		}
		if next == nil || run.RunAt.Before(next.RunAt) ||
			(run.RunAt.Equal(next.RunAt) && run.ID < next.ID) {
			next = run
		}
	}
	return next, nil
}

func (s *fakeScheduleStore) DeleteScheduledRun(_ context.Context, id string) error {
	delete(s.runs, id)
	return nil
}

func TestNewScheduleCmd_AddPersistsDelayedRun(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()

	cmd := NewScheduleCmd(st, nil, func() time.Time { return now })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"add", "Payments/List", "--at", "10m"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)
	require.Len(t, st.runs, 1)
	for _, run := range st.runs {
		assert.Equal(t, "req-1", run.RequestID)
		assert.Equal(t, now.Add(10*time.Minute), run.RunAt)
		assert.Equal(t, domain.ScheduledRunPending, run.Status)
	}
	assert.Contains(t, out.String(), "Scheduled Payments/List")
}

// schedule add must resolve the same paths as `quark run`, including requests
// inside nested collections.
func TestNewScheduleCmd_AddResolvesNestedCollectionPath(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{ID: "parent", Name: "Parent"}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{
		ID: "child", Name: "Child", ParentID: "parent",
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		ID: "nested-req", CollectionID: "child", Name: "Ping",
		Method: http.MethodGet, URL: "https://example.test/ping",
	}))

	cmd := NewScheduleCmd(st, exec.New(http.DefaultTransport), func() time.Time { return now })
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"add", "Parent/Child/Ping", "--at", "10m"})
	cmd.SetContext(ctx)
	require.NoError(t, cmd.Execute())

	runs, err := st.ListScheduledRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "nested-req", runs[0].RequestID)
}

func TestNewScheduleCmd_RunDueExecutesAndMarksCompleted(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID:        "run-1",
		RequestID: "req-1",
		RunAt:     now.Add(-time.Minute),
		Status:    domain.ScheduledRunPending,
	}
	transport := &recordingRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
		},
	}
	executor := exec.New(transport)

	cmd := NewScheduleCmd(st, executor, func() time.Time { return now })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/list", transport.lastURL)
	assert.Equal(t, domain.ScheduledRunCompleted, st.runs["run-1"].Status)
	assert.Contains(t, out.String(), "ran run-1")
}

// A response larger than the in-memory limit is streamed to a temp file.
// run-due does not show the body, so it must remove that file.
func TestNewScheduleCmd_RunDueRemovesStreamedResponseFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // os.TempDir on Unix
	t.Setenv("TMP", tmp)    // os.TempDir on Windows
	t.Setenv("TEMP", tmp)

	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID: "run-1", RequestID: "req-1", RunAt: now.Add(-time.Minute),
		Status: domain.ScheduledRunPending,
	}
	transport := &recordingRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewBufferString("a body larger than the limit")),
		},
	}
	executor := exec.New(transport, exec.WithMaxResponseSize(4))

	cmd := NewScheduleCmd(st, executor, func() time.Time { return now })
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())
	require.Equal(t, domain.ScheduledRunCompleted, st.runs["run-1"].Status)

	leftovers, err := filepath.Glob(filepath.Join(tmp, "quark-response-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "run-due must clean up streamed response files")
}

func TestNewScheduleCmd_RunDue_ContinuesAfterSaveFailure(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID:        "run-1",
		RequestID: "missing-request",
		RunAt:     now.Add(-time.Minute),
		Status:    domain.ScheduledRunPending,
	}
	st.runs["run-2"] = &domain.ScheduledRun{
		ID:        "run-2",
		RequestID: "req-1",
		RunAt:     now.Add(-time.Minute),
		Status:    domain.ScheduledRunPending,
	}
	st.saveErrByRunID = map[string]error{"run-1": assert.AnError}

	transport := &recordingRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
		},
	}

	cmd := NewScheduleCmd(st, exec.New(transport), func() time.Time { return now })
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err, "a failed run must make run-due exit non-zero")
	assert.Contains(t, out.String(), "failed run-1")
	assert.Equal(t, domain.ScheduledRunCompleted, st.runs["run-2"].Status)
	assert.Contains(t, out.String(), "ran run-2")
}

func newFakeScheduleStore() *fakeScheduleStore {
	global := &domain.Environment{ID: "global", Name: "Global"}
	global.SetVars(map[string]string{})
	return &fakeScheduleStore{
		fakeRunStore: fakeRunStore{
			collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
			requests: map[string][]*domain.Request{
				"col-1": {{
					ID:           "req-1",
					CollectionID: "col-1",
					Name:         "List",
					Method:       "GET",
					URL:          "https://example.test/list",
				}},
			},
			globalEnv:   global,
			envsByID:    map[string]*domain.Environment{},
			envsByCol:   map[string][]*domain.Environment{},
			activeEnvID: map[string]string{},
		},
		runs: make(map[string]*domain.ScheduledRun),
	}
}

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
	require.Error(t, cmd.Execute(), "an HTTP 500 is a failed run and must exit non-zero")

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
	cmd := NewScheduleCmd(orderedScheduleStore{st}, exec.New(transport),
		func() time.Time { return now })
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())
	_ = cmd.Execute()

	assert.Equal(t, 2, transport.calls,
		"run-2 must still be attempted when run-1's status save fails")
}

// racingScheduleStore simulates a second process: after run-due has listed
// the due runs, the other process marks them running before this one acts.
type racingScheduleStore struct{ *store.Store }

func (s racingScheduleStore) ListDueScheduledRuns(
	ctx context.Context,
	now time.Time,
) ([]*domain.ScheduledRun, error) {
	runs, err := s.Store.ListDueScheduledRuns(ctx, now)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		other := *run
		other.Status = domain.ScheduledRunRunning
		if err := s.SaveScheduledRun(ctx, &other); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

// A run that another process claimed between "list due" and "mark running"
// must not be executed a second time.
func TestRunDue_SkipsRunClaimedByAnotherProcess(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	col := &domain.Collection{ID: "col-1", Name: "Payments"}
	require.NoError(t, st.SaveCollection(ctx, col))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		ID: "req-1", CollectionID: col.ID, Name: "List",
		Method: http.MethodGet, URL: "https://example.test/list",
	}))
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID: "run-1", RequestID: "req-1", RunAt: now.Add(-time.Minute),
	}))

	transport := &recordingRoundTripper{response: &http.Response{
		StatusCode: 200, Status: "200 OK", Header: http.Header{},
		Body: io.NopCloser(bytes.NewBufferString("ok")),
	}}
	cmd := NewScheduleCmd(
		racingScheduleStore{st}, exec.New(transport), func() time.Time { return now },
	)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(ctx)
	require.NoError(t, cmd.Execute())

	assert.Empty(t, transport.lastURL, "a run claimed elsewhere must not be sent again")
	got, err := st.GetScheduledRun(ctx, "run-1")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunRunning, got.Status, "the other process still owns the run")
}

func runScheduleCmd(t *testing.T, st ScheduleStore, args ...string) (string, error) {
	t.Helper()
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	cmd := NewScheduleCmd(st, nil, func() time.Time { return now })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), err
}

func TestNewScheduleCmd_ListShowsRequestPath(t *testing.T) {
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID:        "run-1",
		RequestID: "req-1",
		RunAt:     time.Date(2026, 6, 25, 16, 0, 0, 0, time.UTC),
		Status:    domain.ScheduledRunPending,
	}

	out, err := runScheduleCmd(t, st, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "Payments/List")
	assert.NotContains(t, out, "request=")
	assert.NotContains(t, out, "req-1")
}

func TestNewScheduleCmd_ListShowsNestedRequestPath(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{ID: "parent", Name: "Parent"}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{
		ID: "child", Name: "Child", ParentID: "parent",
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		ID: "nested-req", CollectionID: "child", Name: "Ping",
		Method: http.MethodGet, URL: "https://example.test/ping",
	}))
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID: "abcdef0123456789", RequestID: "nested-req",
		RunAt: time.Date(2026, 6, 25, 16, 0, 0, 0, time.UTC),
	}))

	out, err := runScheduleCmd(t, st, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "abcdef01")
	assert.Contains(t, out, "Parent/Child/Ping")
}

func TestNewScheduleCmd_CancelPendingRunPreventsRunDue(t *testing.T) {
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID:        "run-1",
		RequestID: "req-1",
		RunAt:     time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC),
		Status:    domain.ScheduledRunPending,
	}

	out, err := runScheduleCmd(t, st, "cancel", "run-1")
	require.NoError(t, err)
	assert.Contains(t, out, "Cancelled run-1")
	assert.Equal(t, domain.ScheduledRunCancelled, st.runs["run-1"].Status)

	out, err = runScheduleCmd(t, st, "run-due")
	require.NoError(t, err)
	assert.Contains(t, out, "No due scheduled runs.")
	assert.Equal(t, domain.ScheduledRunCancelled, st.runs["run-1"].Status)
}

func TestNewScheduleCmd_CancelByUniquePrefix(t *testing.T) {
	st := newFakeScheduleStore()
	st.runs["0123456789abcdef"] = &domain.ScheduledRun{
		ID: "0123456789abcdef", RequestID: "req-1", Status: domain.ScheduledRunPending,
	}
	st.runs["fedcba9876543210"] = &domain.ScheduledRun{
		ID: "fedcba9876543210", RequestID: "req-1", Status: domain.ScheduledRunPending,
	}

	out, err := runScheduleCmd(t, st, "cancel", "01234567")
	require.NoError(t, err)
	assert.Contains(t, out, "Cancelled 01234567")
	assert.Equal(t, domain.ScheduledRunCancelled, st.runs["0123456789abcdef"].Status)
	assert.Equal(t, domain.ScheduledRunPending, st.runs["fedcba9876543210"].Status)
}

func TestNewScheduleCmd_CancelRejectsAmbiguousPrefix(t *testing.T) {
	st := newFakeScheduleStore()
	st.runs["0123456789aaaaaa"] = &domain.ScheduledRun{
		ID: "0123456789aaaaaa", RequestID: "req-1", Status: domain.ScheduledRunPending,
	}
	st.runs["0123456789bbbbbb"] = &domain.ScheduledRun{
		ID: "0123456789bbbbbb", RequestID: "req-1", Status: domain.ScheduledRunPending,
	}

	_, err := runScheduleCmd(t, st, "cancel", "01234567")
	require.ErrorContains(t, err, "ambiguous")
	assert.Equal(t, domain.ScheduledRunPending, st.runs["0123456789aaaaaa"].Status)
	assert.Equal(t, domain.ScheduledRunPending, st.runs["0123456789bbbbbb"].Status)
}

func TestNewScheduleCmd_CancelRejectsShortPrefixAndUnknownRun(t *testing.T) {
	st := newFakeScheduleStore()
	st.runs["0123456789abcdef"] = &domain.ScheduledRun{
		ID: "0123456789abcdef", RequestID: "req-1", Status: domain.ScheduledRunPending,
	}

	_, err := runScheduleCmd(t, st, "cancel", "0123")
	require.ErrorContains(t, err, "not found")
	_, err = runScheduleCmd(t, st, "cancel", "missing-run")
	require.ErrorContains(t, err, "not found")
	assert.Equal(t, domain.ScheduledRunPending, st.runs["0123456789abcdef"].Status)
}

func TestNewScheduleCmd_CancelRejectsFinishedRun(t *testing.T) {
	st := newFakeScheduleStore()
	st.runs["run-done"] = &domain.ScheduledRun{
		ID: "run-done", RequestID: "req-1", Status: domain.ScheduledRunCompleted,
	}

	_, err := runScheduleCmd(t, st, "cancel", "run-done")
	require.ErrorContains(t, err, "only pending runs can be cancelled")
	assert.Equal(t, domain.ScheduledRunCompleted, st.runs["run-done"].Status)
}

func TestNewScheduleCmd_CancelAlreadyCancelledIsNoop(t *testing.T) {
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID: "run-1", RequestID: "req-1", Status: domain.ScheduledRunCancelled,
	}

	out, err := runScheduleCmd(t, st, "cancel", "run-1")
	require.NoError(t, err)
	assert.Contains(t, out, "Already cancelled run-1")
}
