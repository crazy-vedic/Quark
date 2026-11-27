//go:build e2e

package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// Runs are recorded by `quark run`, and `quark history` lists and shows them
// from the same database.
func TestCLI_HistoryListsAndShowsRecordedRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace", "abc")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Ping",
		Method: http.MethodGet,
		URL:    srv.URL,
	})
	for range 2 {
		out, errOut, code := runQuarkWithHome(t, home, "run", "API/Ping")
		require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	}

	out, errOut, code := runQuarkWithHome(t, home, "history", "API/Ping")
	require.Equal(t, 0, code, "history failed: %s %s", out, errOut)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3, "a header and the two runs: %s", out)
	id := strings.Fields(lines[1])[0]

	out, errOut, code = runQuarkWithHome(t, home, "history", "API/Ping", id[:8])
	require.Equal(t, 0, code, "history show failed: %s %s", out, errOut)
	assert.Contains(t, out, "Run:       "+id)
	assert.Contains(t, out, "Status:    200")
	assert.Contains(t, out, "X-Trace: abc")
	assert.Contains(t, out, `"ok": true`)
}
