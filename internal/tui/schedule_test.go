package tui_test

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/tui"
)

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

func TestSchedulePrompt_SavesPendingRunWithFixedClock(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	cfg := config.Default("")
	scheduler := &fakeScheduler{}
	m := tui.New(tui.Deps{
		Config:    cfg,
		Scheduler: scheduler,
		Resolver:  keybindings.NewResolver(cfg.Keybindings),
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	})
	m = m.WithFocus(tui.RequestPane).WithActiveRequest(&domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	})

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	require.Equal(t, tui.ScheduleMode, m.Mode())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10m")})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	msg := cmd()
	model = update(t, model, msg)

	require.Len(t, scheduler.runs, 1)
	assert.Equal(t, "req-1", scheduler.runs[0].RequestID)
	assert.Equal(t, now.Add(10*time.Minute), scheduler.runs[0].RunAt)
	assert.Equal(t, domain.ScheduledRunPending, scheduler.runs[0].Status)
	assert.Contains(t, model.StatusSuccess(), "Scheduled for")
}

func TestScheduleTimer_StaleWakeIsIgnored(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	}
	scheduler := &fakeScheduler{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests:    map[string]*domain.Request{req.ID: req},
		runs: []*domain.ScheduledRun{{
			ID:        "run-1",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Minute),
			Status:    domain.ScheduledRunPending,
		}},
	}
	executor := &recordingExecutor{}
	m := tui.New(tui.Deps{
		Config:    config.Default(""),
		Lister:    scheduler,
		Scheduler: scheduler,
		Reader:    scheduler,
		Executor:  executor,
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	}).WithScheduleTimerSeq(2)

	_, cmd := m.Update(tui.ScheduledRunWakeMsg(1))

	require.Nil(t, cmd)
	assert.Equal(t, 0, executor.calls)
	assert.Equal(t, domain.ScheduledRunPending, scheduler.runs[0].Status)
}

func TestScheduleTimer_MissedRunShowsRetryWarning(t *testing.T) {
	m := tui.New(tui.Deps{Config: config.Default(""), Ctx: context.Background()}).
		WithScheduleTimerSeq(4)

	updated, cmd := m.Update(tui.ScheduledRunMissedMsg(4, "List Payments"))
	require.NotNil(t, cmd)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	assert.Contains(
		t,
		model.StatusErr(),
		`We missed executing your scheduled request "List Payments"`,
	)
}

func TestScheduleTimer_ExecutesDueRunAndShowsBackgroundStatus(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	}
	scheduler := &fakeScheduler{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests:    map[string]*domain.Request{req.ID: req},
		runs: []*domain.ScheduledRun{{
			ID:        "run-1",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Minute),
			Status:    domain.ScheduledRunPending,
		}},
	}
	executor := &recordingExecutor{}
	m := tui.New(tui.Deps{
		Config:    config.Default(""),
		Lister:    scheduler,
		Scheduler: scheduler,
		Reader:    scheduler,
		Executor:  executor,
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	}).WithScheduleTimerSeq(7).WithActiveRequest(req)

	updated, cmd := m.Update(tui.ScheduledRunWakeMsg(7))
	require.NotNil(t, cmd)
	msg := cmd()
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	updated, _ = model.Update(msg)
	model, ok = updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, 1, executor.calls)
	require.Len(t, executor.reqs, 1)
	assert.Equal(t, req.ID, executor.reqs[0].ID)
	assert.Equal(t, domain.ScheduledRunCompleted, scheduler.runs[0].Status)
	assert.Contains(t, model.StatusSuccess(), `Sent "Payments / List Payments" in the background`)
	require.NotNil(t, model.Response())
	assert.Equal(t, 200, model.Response().StatusCode)
}

func TestScheduleTimer_BackgroundFailureIncludesStatusSaveError(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	}
	scheduler := &fakeScheduler{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests:    map[string]*domain.Request{},
		runs: []*domain.ScheduledRun{{
			ID:        "run-1",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Minute),
			Status:    domain.ScheduledRunPending,
		}},
		saveErrByID: map[string]error{"run-1": assert.AnError},
	}
	m := tui.New(tui.Deps{
		Config:    config.Default(""),
		Lister:    scheduler,
		Scheduler: scheduler,
		Reader:    scheduler,
		Executor:  &recordingExecutor{},
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	}).WithScheduleTimerSeq(0)

	updated, cmd := m.Update(tui.ScheduledRunWakeMsg(0))
	require.NotNil(t, cmd)
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	msg := cmd()
	model = update(t, model, msg)

	require.NotEmpty(t, model.StatusErr())
	assert.Contains(t, model.StatusErr(), "failed")
	assert.Contains(t, model.StatusErr(), assert.AnError.Error())
	assert.Equal(t, 1, scheduler.saveCalls["run-1"])
}
