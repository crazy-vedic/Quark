package tui_test

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

const col1 = "col-1"

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("github.com/dlclark/regexp2/v2.runClock"),
	)
}

func newTestModel() tui.Model {
	return tui.New(tui.Deps{})
}

func newModel(cfg config.Config) tui.Model {
	return tui.New(tui.Deps{Config: cfg})
}

func defaultConfig() config.Config {
	cfg := config.Default("")
	cfg.UI.DefaultMethod = "GET"
	return cfg
}

func callUpdate(t *testing.T, m tui.Model, msg tea.Msg) tui.Model {
	t.Helper()
	updated, _ := m.Update(msg)
	model, ok := updated.(tui.Model)
	require.True(t, ok, "Update must return tui.Model")
	return model
}

type captureExecutor struct {
	last *domain.Request
}

func (e *captureExecutor) Execute(
	_ context.Context,
	req *domain.Request,
) (*exec.ExecuteResult, error) {
	cloned := *req
	e.last = &cloned
	return &exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       []byte(`{"ok":true}`),
		Duration:   5 * time.Millisecond,
		Size:       11,
	}, nil
}

type fakeEnvReader struct {
	global *domain.Environment
	envs   map[string]*domain.Environment
	byCol  map[string][]*domain.Environment
}

func (r *fakeEnvReader) GetEnvironment(_ context.Context, id string) (*domain.Environment, error) {
	if env, ok := r.envs[id]; ok {
		return env, nil
	}
	return nil, errors.New("not found")
}

func (r *fakeEnvReader) GetGlobalEnvironment(context.Context) (*domain.Environment, error) {
	return r.global, nil
}

func (r *fakeEnvReader) ListEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return r.byCol[collectionID], nil
}

func (r *fakeEnvReader) ListCollectionEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return r.byCol[collectionID], nil
}

func (r *fakeEnvReader) ListAllEnvironments(context.Context) ([]*domain.Environment, error) {
	var all []*domain.Environment
	for _, envs := range r.byCol {
		all = append(all, envs...)
	}
	return all, nil
}

type fakeScheduler struct {
	collections []*domain.Collection
	runs        []*domain.ScheduledRun
	requests    map[string]*domain.Request
	saveErrByID map[string]error
	saveCalls   map[string]int
}

func (s *fakeScheduler) SaveScheduledRun(_ context.Context, run *domain.ScheduledRun) error {
	if s.saveCalls == nil {
		s.saveCalls = make(map[string]int)
	}
	s.saveCalls[run.ID]++
	if err := s.saveErrByID[run.ID]; err != nil {
		return err
	}
	cloned := *run
	for i, existing := range s.runs {
		if existing.ID == run.ID {
			s.runs[i] = &cloned
			return nil
		}
	}
	s.runs = append(s.runs, &cloned)
	return nil
}

func (s *fakeScheduler) DeleteScheduledRun(_ context.Context, _ string) error {
	return nil
}

func (s *fakeScheduler) GetScheduledRun(
	_ context.Context,
	id string,
) (*domain.ScheduledRun, error) {
	for _, run := range s.runs {
		if run.ID == id {
			cloned := *run
			return &cloned, nil
		}
	}
	return nil, assert.AnError
}

func (s *fakeScheduler) ListScheduledRuns(context.Context) ([]*domain.ScheduledRun, error) {
	return cloneScheduledRuns(s.runs), nil
}

func (s *fakeScheduler) ListCollections(context.Context) ([]*domain.Collection, error) {
	out := make([]*domain.Collection, 0, len(s.collections))
	for _, col := range s.collections {
		cloned := *col
		out = append(out, &cloned)
	}
	return out, nil
}

func (s *fakeScheduler) ListDueScheduledRuns(
	_ context.Context,
	now time.Time,
) ([]*domain.ScheduledRun, error) {
	var due []*domain.ScheduledRun
	for _, run := range s.runs {
		if run.Status == domain.ScheduledRunPending && !run.RunAt.After(now) {
			cloned := *run
			due = append(due, &cloned)
		}
	}
	return due, nil
}

func (s *fakeScheduler) NextPendingScheduledRun(context.Context) (*domain.ScheduledRun, error) {
	var next *domain.ScheduledRun
	for _, run := range s.runs {
		if run.Status != domain.ScheduledRunPending {
			continue
		}
		if next == nil || run.RunAt.Before(next.RunAt) ||
			(run.RunAt.Equal(next.RunAt) && run.ID < next.ID) {
			cloned := *run
			next = &cloned
		}
	}
	return next, nil
}

func (s *fakeScheduler) GetRequest(_ context.Context, id string) (*domain.Request, error) {
	req := s.requests[id]
	if req == nil {
		return nil, assert.AnError
	}
	cloned := *req
	return &cloned, nil
}

func (s *fakeScheduler) ListRequests(
	_ context.Context,
	collectionID string,
) ([]*domain.Request, error) {
	var out []*domain.Request
	for _, req := range s.requests {
		if req.CollectionID == collectionID {
			cloned := *req
			out = append(out, &cloned)
		}
	}
	return out, nil
}

func cloneScheduledRuns(in []*domain.ScheduledRun) []*domain.ScheduledRun {
	out := make([]*domain.ScheduledRun, 0, len(in))
	for _, run := range in {
		cloned := *run
		out = append(out, &cloned)
	}
	return out
}

type recordingExecutor struct {
	calls int
	reqs  []*domain.Request
}

func (e *recordingExecutor) Execute(
	_ context.Context,
	req *domain.Request,
) (*exec.ExecuteResult, error) {
	e.calls++
	cloned := *req
	e.reqs = append(e.reqs, &cloned)
	return &exec.ExecuteResult{StatusCode: 200}, nil
}
