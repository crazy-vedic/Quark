//go:build e2e

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestCLI_Schedule_AddRunDuePersistsHistory(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	quarkDir := filepath.Join(home, ".quark")
	dbPath := filepath.Join(quarkDir, "quark.db")
	require.NoError(t, os.MkdirAll(quarkDir, 0o700))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/payments", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"queued":true}`))
	}))
	t.Cleanup(srv.Close)

	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	col := &domain.Collection{ID: "col-cli-schedule", Name: "Payments"}
	req := &domain.Request{
		ID:           "req-cli-schedule",
		CollectionID: col.ID,
		Name:         "List",
		Method:       "GET",
		URL:          srv.URL + "/payments",
	}
	require.NoError(t, st.SaveCollection(ctx, col))
	require.NoError(t, st.SaveRequest(ctx, req))
	require.NoError(t, st.Close())

	runAt := time.Now().Add(-time.Minute).Format(time.RFC3339)
	out, stderr, code := runQuarkWithHome(
		t,
		home,
		"schedule",
		"add",
		"Payments/List",
		"--at",
		runAt,
	)
	require.Equal(t, 0, code, "schedule add must succeed: stdout=%s stderr=%s", out, stderr)
	assert.Contains(t, out, "Scheduled Payments/List")

	out, stderr, code = runQuarkWithHome(t, home, "schedule", "run-due")
	require.Equal(t, 0, code, "schedule run-due must succeed: stdout=%s stderr=%s", out, stderr)
	assert.Contains(t, out, "request=req-cli-schedule")

	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	runs, err := st.ListScheduledRuns(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, req.ID, runs[0].RequestID)
	assert.Equal(t, domain.ScheduledRunCompleted, runs[0].Status)

	executions, err := st.ListExecutionsByRequest(ctx, req.ID)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	assert.Equal(t, 202, executions[0].StatusCode)
	assert.Contains(t, executions[0].ResponseBody, `"queued":true`)
}
