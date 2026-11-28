//go:build e2e

package tui_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

// The history limit comes from [history] keep_last in config.toml and is
// handed to the store the way cmd/quark does it. Each send from the TUI must
// leave only the newest keep_last executions of that request.
func TestE2E_HistoryRetention_TUIShowsOnlyKeptExecutions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[history]\nkeep_last = 2\n"), 0o600))
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, 2, cfg.History.KeepLast)

	st, err := store.New(filepath.Join(t.TempDir(), "history.db"),
		store.WithCacheSize(100), store.WithHistoryKeepLast(cfg.History.KeepLast))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	var attempt atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempt.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200 + int(n))
		fmt.Fprintf(w, `{"attempt":%d}`, n)
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: uuid.NewString(), Name: "Retention"}
	require.NoError(t, st.SaveCollection(context.Background(), col))
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Poll",
		Method:       "GET",
		URL:          srv.URL + "/poll",
	}
	seedRequests(t, st, col.ID, req)

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport, exec.WithExecutionWriter(st))

	m := newE2EModelWithConfig(t, st, executor, cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})

	for i := 0; i < 3; i++ {
		var cmd tea.Cmd
		m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
		msg := runCmd(t, cmd)
		require.NotNil(t, msg)
		m, cmd = callUpdateWithCmd(t, m, msg)
		msg = runCmd(t, cmd)
		require.NotNil(t, msg)
		m = callUpdate(t, m, msg)
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	}
	require.Equal(t, int32(3), attempt.Load())

	stored, err := st.ListExecutionsByRequest(context.Background(), req.ID)
	require.NoError(t, err)
	require.Len(t, stored, 2, "only keep_last executions survive the store")
	assert.Equal(t, 203, stored[0].StatusCode, "newest execution is kept")
	assert.Equal(t, 202, stored[1].StatusCode)

	require.Len(t, m.Executions(), 2)
	assert.Equal(t, 203, m.Executions()[0].StatusCode)
	assert.Equal(t, 202, m.Executions()[1].StatusCode)
	assertViewContains(t, m, "Run 1/2")
}

func TestE2E_HistoryRetention_SaveReportsFailureOnClosedStore(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "closed.db"),
		store.WithCacheSize(100), store.WithHistoryKeepLast(1))
	require.NoError(t, err)
	require.NoError(t, st.Close())

	now := time.Now()
	err = st.SaveExecution(context.Background(), &domain.Execution{
		ID:              uuid.NewString(),
		RequestID:       "req-1",
		RequestSnapshot: "{}",
		StatusCode:      200,
		StartedAt:       now,
		CompletedAt:     now,
	})
	require.Error(t, err, "a save on a closed store must fail, not report success")
	assert.Contains(t, err.Error(), "save execution")
}
