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
	require.NoError(t, st.SaveScheduledRun(ctx, &domain.ScheduledRun{
		ID:        "run-wall-clock",
		RequestID: req.ID,
		RunAt:     time.Now().Add(80 * time.Millisecond),
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
	elapsed := time.Since(started)
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond)
	assert.Less(t, elapsed, 2*time.Second)

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
