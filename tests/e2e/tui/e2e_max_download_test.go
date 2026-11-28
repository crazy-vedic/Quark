//go:build e2e

package tui_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// loadHTTPConfig writes a config.toml whose [http] table sets max_download to
// the given TOML literal and loads it the same way the application does.
func loadHTTPConfig(t *testing.T, maxDownloadLiteral string) (config.Config, error) {
	t.Helper()
	dir := t.TempDir()
	content := "[http]\nmax_download = " + maxDownloadLiteral + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600))
	return config.Load(dir)
}

// sendDownloadThroughTUI sends a GET for a server that replies with body. The
// executor is wired the way cmd/quark does it, from the loaded config. When
// tmpDir is set, TMPDIR points there while the response is read, so any temp
// file the executor leaves behind is visible to the test.
func sendDownloadThroughTUI(t *testing.T, cfg config.Config, body []byte, tmpDir string) tui.Model {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: uuid.NewString(), Name: "Downloads"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Fetch file",
		Method:       "GET",
		URL:          srv.URL + "/file",
	}
	seedRequests(t, st, col.ID, req)

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport, exec.WithMaxDownload(cfg.MaxDownload()))

	m := newE2EModelWithConfig(t, st, executor, cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	require.True(t, m.Loading())

	if tmpDir != "" {
		t.Setenv("TMPDIR", tmpDir)
	}
	return executeCmdUpdate(t, m, cmd)
}

func TestE2E_MaxDownload_ConfigLimitRejectsInMemoryBody(t *testing.T) {
	cfg, err := loadHTTPConfig(t, `"64KB"`)
	require.NoError(t, err)
	require.Equal(t, int64(64<<10), cfg.MaxDownload())

	m := sendDownloadThroughTUI(t, cfg, bytes.Repeat([]byte("a"), 100<<10), "")

	assert.Nil(t, m.Response(), "an oversized body must not reach the response pane")
	assert.ErrorIs(t, m.Err(), exec.ErrDownloadTooLarge)
	assertViewContains(t, m, "exceeds http.max_download")
}

func TestE2E_MaxDownload_BodyAtExactLimitIsAccepted(t *testing.T) {
	cfg, err := loadHTTPConfig(t, `"64KB"`)
	require.NoError(t, err)

	m := sendDownloadThroughTUI(t, cfg, bytes.Repeat([]byte("b"), 64<<10), "")

	require.NoError(t, m.Err())
	require.NotNil(t, m.Response())
	assert.Equal(t, 200, m.Response().StatusCode)
	assert.Equal(t, int64(64<<10), m.Response().Size)
	assert.Equal(t, int64(64<<10), int64(len(m.Response().Body)))
}

func TestE2E_MaxDownload_StreamedBodyAboveLimitLeavesNoTempFile(t *testing.T) {
	// 11 MiB is past the in-memory threshold, so the body is streamed to a temp
	// file. A 10 MiB limit must reject it and remove the partial file.
	cfg, err := loadHTTPConfig(t, `"10MiB"`)
	require.NoError(t, err)
	tmpDir := t.TempDir()

	m := sendDownloadThroughTUI(t, cfg, bytes.Repeat([]byte("c"), 11<<20), tmpDir)

	assert.Nil(t, m.Response())
	assert.ErrorIs(t, m.Err(), exec.ErrDownloadTooLarge)
	assertViewContains(t, m, "exceeds http.max_download")

	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the partial download must be removed from disk")
}

func TestE2E_MaxDownload_StreamedBodyAtExactLimitIsKept(t *testing.T) {
	cfg, err := loadHTTPConfig(t, `"11MiB"`)
	require.NoError(t, err)
	tmpDir := t.TempDir()

	m := sendDownloadThroughTUI(t, cfg, bytes.Repeat([]byte("d"), 11<<20), tmpDir)

	require.NoError(t, m.Err())
	require.NotNil(t, m.Response())
	assert.Equal(t, int64(11<<20), m.Response().Size)
	require.NotEmpty(t, m.Response().TempPath, "a streamed body is kept in a temp file")
	info, err := os.Stat(m.Response().TempPath)
	require.NoError(t, err)
	assert.Equal(t, int64(11<<20), info.Size())
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "response bodies are private")
}

func TestE2E_MaxDownload_ConfigValueForms(t *testing.T) {
	tests := []struct {
		name    string
		literal string
		want    int64
	}{
		{"zero disables the limit", "0", 0},
		{"integer is bytes", "2048", 2048},
		{"plain string is bytes", `"512"`, 512},
		{"lower-case KB suffix", `"10kb"`, 10 << 10},
		{"KiB suffix with space", `"2 KiB"`, 2 << 10},
		{"MiB suffix", `"5MiB"`, 5 << 20},
		{"GB suffix", `"1GB"`, 1 << 30},
		{"explicit B suffix", `"7B"`, 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadHTTPConfig(t, tc.literal)
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.MaxDownload())
		})
	}
}

func TestE2E_MaxDownload_InvalidConfigValuesFailToLoad(t *testing.T) {
	tests := []struct {
		name    string
		literal string
		wantErr string
	}{
		{"negative integer", "-1", "must not be negative"},
		{"unparseable string", `"bogus"`, "invalid size"},
		{"negative string", `"-5KB"`, "invalid size"},
		{"overflowing string", `"99999999999GB"`, "is too large"},
		{"boolean is not a size", "true", "size must be an integer"},
		{"float is not a size", "1.5", "size must be an integer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadHTTPConfig(t, tc.literal)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestE2E_MaxDownload_DefaultLimitWithoutConfig(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, int64(1<<30), cfg.MaxDownload())
}
