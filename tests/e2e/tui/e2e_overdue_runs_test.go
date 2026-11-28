//go:build e2e

package tui_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// overdueClock is the TUI's notion of "now" for the overdue-runs tests. Every
// run seeded below is due before it, so the startup check finds them.
var overdueClock = time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)

func seedOverdueRun(t *testing.T, st *store.Store, id, requestID string, runAt time.Time) {
	t.Helper()
	require.NoError(t, st.SaveScheduledRun(context.Background(), &domain.ScheduledRun{
		ID:        id,
		RequestID: requestID,
		RunAt:     runAt,
		Status:    domain.ScheduledRunPending,
	}))
}

// openOverdueModal builds the TUI as it starts up and runs its Init commands.
// The overdue-runs prompt must be the mode it lands in.
func openOverdueModal(t *testing.T, st *store.Store, executor tui.RequestExecutor) tui.Model {
	t.Helper()
	cfg := config.Default("")
	m := tui.New(tui.Deps{
		Lister:          st,
		Reader:          st,
		Writer:          st,
		ColWriter:       st,
		ExecutionReader: st,
		EnvReader:       st,
		EnvWriter:       st,
		Scheduler:       st,
		ActiveEnvStore:  st,
		Executor:        executor,
		Config:          cfg,
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
		Ctx:             context.Background(),
		Now:             func() time.Time { return overdueClock },
	})
	m = resize(t, m, 120, 40)
	cmd := m.Init()
	require.NotNil(t, cmd)
	msg := runCmd(t, cmd)
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		m = callUpdate(t, m, msg)
	} else {
		for _, batched := range batch {
			if batched == nil {
				continue
			}
			if next := runCmd(t, batched); next != nil {
				m = callUpdate(t, m, next)
			}
		}
	}
	require.Equal(t, tui.OverdueRunsMode, m.Mode(), "due runs at startup must open the prompt")
	return m
}

func pressKey(t *testing.T, m tui.Model, key tea.KeyMsg) tui.Model {
	t.Helper()
	return callUpdate(t, m, key)
}

func runStatus(t *testing.T, st *store.Store, id string) string {
	t.Helper()
	run, err := st.GetScheduledRun(context.Background(), id)
	require.NoError(t, err)
	return run.Status
}

// Choosing "Run all now" sends every overdue run and marks each one completed.
func TestE2E_OverdueRuns_RunAllSendsEachOverdueRun(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"sent":true}`))
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: "overdue-col", Name: "Billing"}
	st := setupStore(t, col)
	first := &domain.Request{ID: "overdue-a", CollectionID: col.ID, Name: "Invoices", Method: "GET", URL: srv.URL + "/a"}
	second := &domain.Request{ID: "overdue-b", CollectionID: col.ID, Name: "Refunds", Method: "GET", URL: srv.URL + "/b"}
	seedRequests(t, st, col.ID, first, second)
	seedOverdueRun(t, st, "run-a", first.ID, overdueClock.Add(-2*time.Hour))
	seedOverdueRun(t, st, "run-b", second.ID, overdueClock.Add(-time.Hour))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := openOverdueModal(t, st, exec.New(transport, exec.WithExecutionWriter(st)))

	assertViewContains(t, m, "2 overdue scheduled runs became due while Quark was closed")
	assertViewContains(t, m, "> Run all now")
	assertViewContains(t, m, "Skip: cancel these runs")
	assertViewContains(t, m, "Decide later: keep them pending, do not run this session")
	assertViewContains(t, m, "Billing / Invoices")

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, tui.NormalMode, m.Mode())
	m = callUpdate(t, m, runCmd(t, cmd))

	assert.Equal(t, domain.ScheduledRunCompleted, runStatus(t, st, "run-a"))
	assert.Equal(t, domain.ScheduledRunCompleted, runStatus(t, st, "run-b"))
	assert.Equal(t, int32(2), hits.Load(), "each overdue run must be sent exactly once")
	executions, err := st.ListExecutionsByRequest(context.Background(), first.ID)
	require.NoError(t, err)
	assert.Len(t, executions, 1)
	assert.NotEmpty(t, m.StatusSuccess())
}

// Choosing "Skip" cancels the runs; they are never sent.
func TestE2E_OverdueRuns_SkipCancelsWithoutSending(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: "skip-col", Name: "Reports"}
	st := setupStore(t, col)
	req := &domain.Request{ID: "skip-req", CollectionID: col.ID, Name: "Export", Method: "GET", URL: srv.URL}
	seedRequests(t, st, col.ID, req)
	seedOverdueRun(t, st, "run-skip", req.ID, overdueClock.Add(-time.Hour))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := openOverdueModal(t, st, exec.New(transport, exec.WithExecutionWriter(st)))

	// Down moves to "Skip", up returns to "Run all now", down again selects "Skip".
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assertViewContains(t, m, "> Skip: cancel these runs")
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyDown})

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, runCmd(t, cmd))

	assert.Equal(t, "Cancelled 1 overdue scheduled run", m.StatusSuccess())
	assert.Equal(t, domain.ScheduledRunCancelled, runStatus(t, st, "run-skip"))
	assert.Zero(t, hits.Load(), "a skipped run must not be sent")
	assert.Equal(t, tui.NormalMode, m.Mode())
}

// Skipping a run that another process already completed leaves it completed.
func TestE2E_OverdueRuns_SkipKeepsRunsClaimedElsewhere(t *testing.T) {
	col := &domain.Collection{ID: "claimed-col", Name: "Ops"}
	st := setupStore(t, col)
	req := &domain.Request{ID: "claimed-req", CollectionID: col.ID, Name: "Ping", Method: "GET", URL: "http://example.invalid"}
	seedRequests(t, st, col.ID, req)
	seedOverdueRun(t, st, "run-claimed", req.ID, overdueClock.Add(-time.Hour))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := openOverdueModal(t, st, exec.New(transport, exec.WithExecutionWriter(st)))

	// Another quark process (for example `run-due`) sends the run while the prompt is open.
	require.NoError(t, st.SaveScheduledRun(context.Background(), &domain.ScheduledRun{
		ID: "run-claimed", RequestID: req.ID, RunAt: overdueClock.Add(-time.Hour),
		Status: domain.ScheduledRunCompleted,
	}))

	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, runCmd(t, cmd))

	assert.Equal(t, "Cancelled 0 overdue scheduled runs", m.StatusSuccess())
	assert.Equal(t, domain.ScheduledRunCompleted, runStatus(t, st, "run-claimed"))
}

// Esc and "Decide later" keep the runs pending and do not send them this session.
func TestE2E_OverdueRuns_DecideLaterKeepsRunsPending(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: "later-col", Name: "Later"}
	st := setupStore(t, col)
	req := &domain.Request{ID: "later-req", CollectionID: col.ID, Name: "Later", Method: "GET", URL: srv.URL}
	seedRequests(t, st, col.ID, req)
	seedOverdueRun(t, st, "run-esc", req.ID, overdueClock.Add(-time.Hour))
	seedOverdueRun(t, st, "run-enter", req.ID, overdueClock.Add(-30*time.Minute))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := openOverdueModal(t, st, exec.New(transport, exec.WithExecutionWriter(st)))

	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, "2 overdue scheduled runs left pending; they will not run this session", m.StatusSuccess())
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, st, "run-esc"))
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, st, "run-enter"))
	assert.Zero(t, hits.Load())
}

// The cursor stops at both ends of the list before a choice is made.
func TestE2E_OverdueRuns_CursorClampsAndEnterDecidesLater(t *testing.T) {
	col := &domain.Collection{ID: "clamp-col", Name: "Clamp"}
	st := setupStore(t, col)
	req := &domain.Request{ID: "clamp-req", CollectionID: col.ID, Name: "Clamp", Method: "GET", URL: "http://example.invalid"}
	seedRequests(t, st, col.ID, req)
	seedOverdueRun(t, st, "run-clamp", req.ID, overdueClock.Add(-time.Hour))

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := openOverdueModal(t, st, exec.New(transport, exec.WithExecutionWriter(st)))

	for i := 0; i < 3; i++ {
		m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}
	assertViewContains(t, m, "> Run all now")
	for i := 0; i < 5; i++ {
		m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	assertViewContains(t, m, "> Decide later: keep them pending, do not run this session")

	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, "1 overdue scheduled run left pending; they will not run this session", m.StatusSuccess())
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, st, "run-clamp"))
}

// With no sender configured, "Run all now" reports the failure instead of
// starting anything.
func TestE2E_OverdueRuns_RunAllWithoutExecutorReportsUnavailable(t *testing.T) {
	col := &domain.Collection{ID: "noexec-col", Name: "NoExec"}
	st := setupStore(t, col)
	req := &domain.Request{ID: "noexec-req", CollectionID: col.ID, Name: "Send", Method: "GET", URL: "http://example.invalid"}
	seedRequests(t, st, col.ID, req)
	seedOverdueRun(t, st, "run-noexec", req.ID, overdueClock.Add(-time.Hour))

	m := openOverdueModal(t, st, nil)
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, runCmd(t, cmd))

	assert.Contains(t, m.StatusErr(), "scheduler unavailable")
	assert.Equal(t, domain.ScheduledRunPending, runStatus(t, st, "run-noexec"))
}

// Long lists show the first eight runs and a count of the rest.
func TestE2E_OverdueRuns_LongListAndMissingRequestLabels(t *testing.T) {
	col := &domain.Collection{ID: "long-col", Name: "Long"}
	st := setupStore(t, col)
	req := &domain.Request{ID: "long-req", CollectionID: col.ID, Name: "Repeat", Method: "GET", URL: "http://example.invalid"}
	seedRequests(t, st, col.ID, req)
	for i := 0; i < 10; i++ {
		seedOverdueRun(t, st, fmt.Sprintf("run-long-%02d", i), req.ID, overdueClock.Add(-time.Duration(i+1)*time.Hour))
	}

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := openOverdueModal(t, st, exec.New(transport, exec.WithExecutionWriter(st)))

	assertViewContains(t, m, "10 overdue scheduled runs became due while Quark was closed")
	assertViewContains(t, m, "… and 2 more")

	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, "10 overdue scheduled runs left pending; they will not run this session", m.StatusSuccess())
}
