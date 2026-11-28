//go:build e2e

package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// runAndReadHistory sends one GET for a server that replies with body, then
// reads the stored execution back from the home's database.
func runAndReadHistory(t *testing.T, body []byte) *domain.Execution {
	t.Helper()
	home, colID := setupHomeWithCollection(t)
	srv := newHistoryBodyServer(t, body)
	reqID := uuid.New().String()
	seedRequestWithID(t, home, colID, &domain.Request{
		ID:     reqID,
		Name:   "Fetch",
		Method: http.MethodGet,
		URL:    srv.URL + "/file",
	})

	_, stderr, code := runQuarkWithHome(t, home, "run", "API/Fetch")
	require.Equal(t, 0, code, "run must succeed: %s", stderr)

	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	ctx := context.Background()
	list, err := st.ListExecutionsByRequest(ctx, reqID, 0)
	require.NoError(t, err)
	require.Len(t, list, 1, "run must record exactly one execution")
	ex, err := st.GetExecution(ctx, list[0].ID)
	require.NoError(t, err)
	return ex
}

// seedRequestWithID saves a request under a caller-chosen ID so the test can
// find its execution history afterwards.
func seedRequestWithID(t *testing.T, home, colID string, req *domain.Request) {
	t.Helper()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	req.CollectionID = colID
	require.NoError(t, st.SaveRequest(context.Background(), req))
}

func TestCLI_HistoryStoresInMemoryBodyCutAtLimitOnRuneBoundary(t *testing.T) {
	// Each "€" is 3 bytes, so 2 MiB of them ends 1 byte past the 1 MiB history
	// limit (1 MiB is not a multiple of 3). The cut must drop that partial rune.
	const limit = 1 << 20
	body := []byte(strings.Repeat("€", (2<<20)/3))

	ex := runAndReadHistory(t, body)

	assert.True(t, ex.ResponseBodyTruncated, "a body over the history limit is marked truncated")
	assert.Equal(t, int64(len(body)), ex.ResponseSize, "the recorded size is the full body")
	assert.Equal(t, limit-1, len(ex.ResponseBody), "the stored prefix ends before the split rune")
	assert.True(t, utf8.ValidString(ex.ResponseBody), "the stored prefix stays valid text")
}

func TestCLI_HistoryStoresStreamedBodyOnlyUpToLimit(t *testing.T) {
	// 11 MiB exceeds the in-memory threshold, so the body is streamed to a temp
	// file and only its first 1 MiB may be read back into history. The bytes
	// are invalid UTF-8, so nothing is trimmed at the cut.
	const limit = 1 << 20
	body := bytes.Repeat([]byte{0xff}, 11<<20)

	ex := runAndReadHistory(t, body)

	assert.True(t, ex.ResponseBodyTruncated)
	assert.Equal(t, int64(len(body)), ex.ResponseSize)
	assert.Len(t, ex.ResponseBody, limit)
}

func TestCLI_HistoryStoresSmallBodyWhole(t *testing.T) {
	ex := runAndReadHistory(t, []byte(`{"ok":true}`))

	assert.False(t, ex.ResponseBodyTruncated)
	assert.Equal(t, int64(len(`{"ok":true}`)), ex.ResponseSize)
	assert.Equal(t, `{"ok":true}`, ex.ResponseBody)
}

// newHistoryBodyServer serves body with 200 OK for every request.
func newHistoryBodyServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
