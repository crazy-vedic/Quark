//go:build e2e

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/tests/e2e"
)

// writeHTTPConfig sets http.max_download in the home's config.toml to the
// given TOML literal.
func writeHTTPConfig(t *testing.T, home, maxDownloadLiteral string) {
	t.Helper()
	configPath := filepath.Join(home, ".quark", "config.toml")
	content := "[http]\nmax_download = " + maxDownloadLiteral + "\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))
}

// newFixedBodyServer serves body with 200 OK for every request.
func newFixedBodyServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCLI_MaxDownloadRejectsResponseOverConfiguredLimit(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	writeHTTPConfig(t, home, `"64KB"`)
	srv := newFixedBodyServer(t, bytes.Repeat([]byte("x"), 100<<10))
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Fetch",
		Method: http.MethodGet,
		URL:    srv.URL + "/file",
	})

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch")

	assert.NotEqual(t, 0, code, "a response over http.max_download must fail the command")
	assert.Contains(t, stderr, "exceeds http.max_download")
	assert.NotContains(t, stdout, "200 OK", "no response is printed for a rejected body")
}

func TestCLI_MaxDownloadStreamedBodyOverLimitLeavesNoTempFile(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	writeHTTPConfig(t, home, `"10MiB"`)
	srv := newFixedBodyServer(t, bytes.Repeat([]byte("y"), 11<<20))
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Fetch",
		Method: http.MethodGet,
		URL:    srv.URL + "/file",
	})
	tmpDir := t.TempDir()

	_, stderr, code := runQuarkWithHomeEnv(t, home,
		[]string{"TMPDIR=" + e2e.EscapePathForJSON(tmpDir)}, "run", "API/Fetch")

	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "exceeds http.max_download")
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the partial download must not stay on disk")
}

func TestCLI_MaxDownloadAllowsResponseWithinLimit(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	writeHTTPConfig(t, home, `"12MiB"`)
	srv := newFixedBodyServer(t, bytes.Repeat([]byte("z"), 11<<20))
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Fetch",
		Method: http.MethodGet,
		URL:    srv.URL + "/file",
	})

	stdout, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch")

	assert.Equal(t, 0, code, "stderr: %s", stderr)
	assert.NotContains(t, stderr, "http.max_download")
	// The body is printed in full, so stdout must carry all 11 MiB.
	assert.True(t, len(stdout) >= 11<<20, "stdout carries %d bytes, want the full 11 MiB body", len(stdout))
}

func TestCLI_MaxDownloadInvalidConfigStopsCommand(t *testing.T) {
	tests := []struct {
		name    string
		literal string
		wantErr string
	}{
		{"negative integer", "-1", "must not be negative"},
		{"unparseable string", `"lots"`, "invalid size"},
		{"overflowing string", `"99999999999GB"`, "is too large"},
		{"boolean", "false", "size must be an integer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := setupHomeWithCollection(t)
			writeHTTPConfig(t, home, tc.literal)

			_, stderr, code := runQuarkWithHome(t, home, "collection", "list")

			assert.NotEqual(t, 0, code, "a bad http.max_download must fail the command")
			assert.Contains(t, stderr, "config.toml")
			assert.Contains(t, stderr, tc.wantErr)
		})
	}
}
