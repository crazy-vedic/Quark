//go:build e2e

package tui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestE2E_ScheduledBackgroundRunPersistsHistory(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	col := &domain.Collection{ID: "col-schedule", Name: "Payments"}
	st := setupStore(t, col)

	req := &domain.Request{
		ID:           "req-schedule",
		CollectionID: col.ID,
		Name:         "List Payments",
		Method:       "GET",
		URL:          "",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/payments", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"queued":true}`))
	}))
	t.Cleanup(srv.Close)

	req.URL = srv.URL + "/payments"
	seedRequests(t, st, col.ID, req)
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID:        "run-schedule",
		RequestID: req.ID,
		RunAt:     fixedNow.Add(-time.Minute),
		Status:    domain.ScheduledRunPending,
	}))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport, exec.WithExecutionWriter(st))
	cfg := config.Default("")
	m := tui.New(tui.Deps{
		Lister:          st,
		Reader:          st,
		ExecutionReader: st,
		EnvReader:       st,
		ActiveEnvStore:  st,
		Scheduler:       st,
		Executor:        executor,
		Config:          cfg,
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
		Ctx:             ctx,
		Now:             func() time.Time { return fixedNow },
	}).WithScheduleTimerSeq(9).WithActiveRequest(req)

	updated, cmd := m.Update(tui.ScheduledRunWakeMsg(9))
	require.NotNil(t, cmd)
	msg := cmd()
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	var followUp tea.Cmd
	updated, followUp = model.Update(msg)
	model, ok = updated.(tui.Model)
	require.True(t, ok)

	run, err := st.GetScheduledRun(ctx, "run-schedule")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunCompleted, run.Status)
	assert.Contains(t, model.StatusSuccess(), `Sent "Payments / List Payments" in the background`)
	require.NotNil(t, model.Response())
	assert.Equal(t, 202, model.Response().StatusCode)
	require.NotNil(t, model.Response().Body)
	assert.Contains(t, string(model.Response().Body), `"queued":true`)

	model = executeScheduleFollowUpCmd(t, model, followUp)
	require.Len(t, model.Executions(), 1)
	assert.Equal(t, 202, model.Executions()[0].StatusCode)

	executions, err := st.ListExecutionsByRequest(ctx, req.ID)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	assert.Equal(t, 202, executions[0].StatusCode)
	assert.Contains(t, executions[0].ResponseBody, `"queued":true`)
}

func TestE2E_ScheduledStartupTimerUsesWallClock(t *testing.T) {
	ctx := context.Background()
	col := &domain.Collection{ID: "col-wall-clock", Name: "Payments"}
	st := setupStore(t, col)

	req := &domain.Request{
		ID:           "req-wall-clock",
		CollectionID: col.ID,
		Name:         "List Payments",
		Method:       "GET",
		URL:          "",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/timer", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	req.URL = srv.URL + "/timer"
	seedRequests(t, st, col.ID, req)
	runAt := time.Now().Add(80 * time.Millisecond)
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID:        "run-wall-clock",
		RequestID: req.ID,
		RunAt:     runAt,
		Status:    domain.ScheduledRunPending,
	}))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport, exec.WithExecutionWriter(st))
	cfg := config.Default("")
	m := tui.New(tui.Deps{
		Lister:         st,
		Reader:         st,
		EnvReader:      st,
		ActiveEnvStore: st,
		Scheduler:      st,
		Executor:       executor,
		Config:         cfg,
		Resolver:       keybindings.NewResolver(cfg.Keybindings),
		Ctx:            ctx,
	})

	cmd := m.Init()
	require.NotNil(t, cmd)
	started := time.Now()
	msg := runCmd(t, cmd)
	var executeCmd tea.Cmd
	model := m
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, batchedCmd := range msg {
			batchedMsg := runCmd(t, batchedCmd)
			if batchedMsg == nil {
				continue
			}
			var updated tea.Model
			updated, executeCmd = model.Update(batchedMsg)
			var ok bool
			model, ok = updated.(tui.Model)
			require.True(t, ok)
		}
	default:
		var updated tea.Model
		updated, executeCmd = model.Update(msg)
		var ok bool
		model, ok = updated.(tui.Model)
		require.True(t, ok)
	}
	// The timer must not fire before RunAt. Measure against RunAt, not the time
	// since startup: slow runners spend part of the 80ms delay in test setup.
	assert.False(t, time.Now().Before(runAt), "timer fired before RunAt")
	assert.Less(t, time.Since(started), 2*time.Second)

	require.NotNil(t, executeCmd)
	resultMsg := runCmd(t, executeCmd)
	model = callUpdate(t, model, resultMsg)

	run, err := st.GetScheduledRun(ctx, "run-wall-clock")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunCompleted, run.Status)
	assert.Contains(t, model.StatusSuccess(), `Sent "Payments / List Payments" in the background`)

	executions, err := st.ListExecutionsByRequest(ctx, req.ID)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	assert.Equal(t, 204, executions[0].StatusCode)
}

func executeScheduleFollowUpCmd(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	t.Helper()
	msg := runCmd(t, cmd)
	switch msg := msg.(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, batchedCmd := range msg {
			batchedMsg := runCmd(t, batchedCmd)
			if batchedMsg != nil {
				m = callUpdate(t, m, batchedMsg)
			}
		}
		return m
	default:
		return callUpdate(t, m, msg)
	}
}

// A process that dies mid-run leaves its run in "running". Recovery marks only
// runs stuck since before the cutoff as failed and leaves everything else alone.
func TestE2E_ScheduledRunsStuckRunningAreRecovered(t *testing.T) {
	ctx := context.Background()
	col := &domain.Collection{ID: "col-recover", Name: "Payments"}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, &domain.Request{
		ID:           "req-recover",
		CollectionID: col.ID,
		Name:         "List",
		Method:       "GET",
		URL:          "http://example.invalid",
	})
	for _, run := range []*domain.ScheduledRun{
		{ID: "stuck", RequestID: "req-recover", RunAt: time.Now(), Status: domain.ScheduledRunRunning},
		{ID: "waiting", RequestID: "req-recover", RunAt: time.Now(), Status: domain.ScheduledRunPending},
	} {
		require.NoError(t, st.SaveScheduledRun(ctx, run))
	}

	// A cutoff in the past: the run only just started, so it is not stale yet.
	n, err := st.RecoverStaleScheduledRuns(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)

	// A cutoff in the future: the running run is older than it.
	n, err = st.RecoverStaleScheduledRuns(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	stuck, err := st.GetScheduledRun(ctx, "stuck")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunFailed, stuck.Status)
	assert.Contains(t, stuck.LastError, "interrupted while running")

	waiting, err := st.GetScheduledRun(ctx, "waiting")
	require.NoError(t, err)
	assert.Equal(t, domain.ScheduledRunPending, waiting.Status)

	// Recovery is idempotent: nothing is left in the running state.
	n, err = st.RecoverStaleScheduledRuns(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
}
