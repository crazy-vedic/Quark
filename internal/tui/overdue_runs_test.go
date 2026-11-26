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
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tuitest"
)

var overdueNow = time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)

func overdueScheduler(runs ...*domain.ScheduledRun) *fakeScheduler {
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	}
	return &fakeScheduler{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests:    map[string]*domain.Request{req.ID: req},
		runs:        runs,
	}
}

func overdueRun(id string, runAt time.Time) *domain.ScheduledRun {
	return &domain.ScheduledRun{
		ID:        id,
		RequestID: "req-1",
		RunAt:     runAt,
		Status:    domain.ScheduledRunPending,
	}
}

// startOverdueModel builds a model and feeds it every message its Init
// command produces, as the Bubble Tea loop would at startup.
func startOverdueModel(
	t *testing.T,
	scheduler *fakeScheduler,
	executor *recordingExecutor,
) tui.Model {
	t.Helper()
	cfg := config.Default("")
	cfg.Updates.Check = false
	m := tui.New(tui.Deps{
		Config:    cfg,
		Lister:    scheduler,
		Scheduler: scheduler,
		Reader:    scheduler,
		Executor:  executor,
		Ctx:       context.Background(),
		Now:       func() time.Time { return overdueNow },
	})
	m = tuitest.Resize(t, m, 120, 40)
	msg := tuitest.RunCmd(t, m.Init())
	msgs := []tea.Msg{msg}
	if batch, ok := msg.(tea.BatchMsg); ok {
		msgs = msgs[:0]
		for _, cmd := range batch {
			msgs = append(msgs, tuitest.RunCmd(t, cmd))
		}
	}
	for _, msg := range msgs {
		if msg != nil {
			m = callUpdate(t, m, msg)
		}
	}
	return m
}

func runStatus(t *testing.T, s *fakeScheduler, id string) string {
	t.Helper()
	run, err := s.GetScheduledRun(context.Background(), id)
	require.NoError(t, err)
	return run.Status
}

func TestOverdueRuns_StartupAsksInsteadOfRunning(t *testing.T) {
	scheduler := overdueScheduler(
		overdueRun("run-1", overdueNow.Add(-48*time.Hour)),
		overdueRun("run-2", overdueNow.Add(time.Hour)),
	)
	executor := &recordingExecutor{}

	m := startOverdueModel(t, scheduler, executor)

	assert.Equal(t, tui.OverdueRunsMode, m.Mode())
	assert.Zero(t, executor.calls)
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, scheduler, "run-1"))
	tuitest.AssertViewContains(t, m, "Overdue scheduled runs")
	tuitest.AssertViewContains(t, m, "Payments / List Payments")
	tuitest.AssertViewContains(t, m, "Run all now")
	tuitest.AssertViewContains(t, m, "Decide later")
}

func TestOverdueRuns_RunAllSendsOverdueRuns(t *testing.T) {
	scheduler := overdueScheduler(
		overdueRun("run-1", overdueNow.Add(-time.Hour)),
		overdueRun("run-2", overdueNow.Add(-time.Minute)),
	)
	executor := &recordingExecutor{}
	m := startOverdueModel(t, scheduler, executor)

	m, cmd := tuitest.UpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NormalMode, m.Mode())
	require.NotNil(t, cmd)
	m = callUpdate(t, m, cmd())

	assert.Equal(t, 2, executor.calls)
	assert.Equal(t, domain.ScheduledRunCompleted, runStatus(t, scheduler, "run-1"))
	assert.Equal(t, domain.ScheduledRunCompleted, runStatus(t, scheduler, "run-2"))
	assert.Contains(t, m.StatusSuccess(), "Sent 2 scheduled requests")
}

func TestOverdueRuns_SkipCancelsRuns(t *testing.T) {
	scheduler := overdueScheduler(overdueRun("run-1", overdueNow.Add(-time.Hour)))
	executor := &recordingExecutor{}
	m := startOverdueModel(t, scheduler, executor)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := tuitest.UpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, cmd())

	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Zero(t, executor.calls)
	assert.Equal(t, domain.ScheduledRunCancelled, runStatus(t, scheduler, "run-1"))
	assert.Contains(t, m.StatusSuccess(), "Cancelled 1 overdue scheduled run")
}

func TestOverdueRuns_SkipReportsSaveError(t *testing.T) {
	scheduler := overdueScheduler(overdueRun("run-1", overdueNow.Add(-time.Hour)))
	m := startOverdueModel(t, scheduler, &recordingExecutor{})
	scheduler.saveErrByID = map[string]error{"run-1": assert.AnError}

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := tuitest.UpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, cmd())

	assert.Contains(t, m.StatusErr(), "Cancel scheduled runs failed")
}

func TestOverdueRuns_DecideLaterKeepsRunsPendingForTheSession(t *testing.T) {
	scheduler := overdueScheduler(overdueRun("run-1", overdueNow.Add(-time.Hour)))
	executor := &recordingExecutor{}
	m := startOverdueModel(t, scheduler, executor)

	m, cmd := tuitest.UpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Contains(t, m.StatusSuccess(), "will not run this session")
	// The deferred run is the only pending run, so nothing is armed.
	require.NotNil(t, cmd)
	assert.Nil(t, cmd())

	// A later wake does not send the deferred run.
	m, cmd = tuitest.UpdateWithCmd(t, m, tui.ScheduledRunWakeMsg(m.ScheduleTimerSeq()))
	require.NotNil(t, cmd)
	_ = callUpdate(t, m, cmd())
	assert.Zero(t, executor.calls)
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, scheduler, "run-1"))
}

func TestOverdueRuns_DecideLaterStillFiresNewlyDueRuns(t *testing.T) {
	scheduler := overdueScheduler(overdueRun("old", overdueNow.Add(-time.Hour)))
	executor := &recordingExecutor{}
	m := startOverdueModel(t, scheduler, executor)

	m, _ = tuitest.UpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	// A run that becomes due while the TUI is open still fires.
	scheduler.runs = append(scheduler.runs, overdueRun("new", overdueNow))
	m, cmd := tuitest.UpdateWithCmd(t, m, tui.ScheduledRunWakeMsg(m.ScheduleTimerSeq()))
	require.NotNil(t, cmd)
	_ = callUpdate(t, m, cmd())

	assert.Equal(t, 1, executor.calls)
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, scheduler, "old"))
	assert.Equal(t, domain.ScheduledRunCompleted, runStatus(t, scheduler, "new"))
}

func TestOverdueRuns_NavigationStaysInBounds(t *testing.T) {
	scheduler := overdueScheduler(overdueRun("run-1", overdueNow.Add(-time.Hour)))
	m := startOverdueModel(t, scheduler, &recordingExecutor{})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = callUpdate(t, m, tuitest.KeyRunes('x'))
	for range 5 {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	assert.Equal(t, tui.OverdueRunsMode, m.Mode())
	// The cursor stopped on the last choice: decide later.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, scheduler, "run-1"))
	assert.Contains(t, m.StatusSuccess(), "will not run this session")
}

func TestOverdueRuns_ListIsCapped(t *testing.T) {
	var runs []*domain.ScheduledRun
	for i := range 10 {
		runAt := overdueNow.Add(-time.Duration(i+1) * time.Minute)
		runs = append(runs, overdueRun(string(rune('a'+i)), runAt))
	}
	m := startOverdueModel(t, overdueScheduler(runs...), &recordingExecutor{})

	tuitest.AssertViewContains(t, m, "10 overdue scheduled runs")
	tuitest.AssertViewContains(t, m, "and 2 more")
}
