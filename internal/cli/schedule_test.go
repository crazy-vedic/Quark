package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
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

func TestNewScheduleCmd_RunDue_ReturnsStatusSaveErrorWhenMarkingFailed(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	st := newFakeScheduleStore()
	st.runs["run-1"] = &domain.ScheduledRun{
		ID:        "run-1",
		RequestID: "missing-request",
		RunAt:     now.Add(-time.Minute),
		Status:    domain.ScheduledRunPending,
	}
	st.saveErrByRunID = map[string]error{"run-1": assert.AnError}

	cmd := NewScheduleCmd(st, nil, func() time.Time { return now })
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"run-due"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "mark run-1 failed after request load error")
	assert.ErrorContains(t, err, assert.AnError.Error())
	assert.Equal(t, 1, st.saveCallCountByID["run-1"])
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
