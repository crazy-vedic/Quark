package exec_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/exec"
)

func executeForHistory(t *testing.T, body string, opts ...exec.Option) *recordingExecutionWriter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)

	writer := &recordingExecutionWriter{}
	opts = append(opts, exec.WithExecutionWriter(writer))
	e := newTestExecutor(transport, opts...)
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))
	require.NoError(t, err)
	t.Cleanup(func() { _ = result.Cleanup() })
	require.Len(t, writer.executions, 1)
	return writer
}

func TestHistory_InMemoryBodyOverLimitIsTruncated(t *testing.T) {
	body := strings.Repeat("a", 100)
	writer := executeForHistory(t, body, exec.WithHistoryBodyLimit(40))

	ex := writer.executions[0]
	assert.Equal(t, body[:40], ex.ResponseBody)
	assert.True(t, ex.ResponseBodyTruncated)
	assert.Equal(t, int64(100), ex.ResponseSize)
}

func TestHistory_StreamedBodyOverLimitIsTruncated(t *testing.T) {
	body := strings.Repeat("b", 4096)
	writer := executeForHistory(t, body,
		exec.WithMaxResponseSize(1024),
		exec.WithHistoryBodyLimit(2000),
	)

	ex := writer.executions[0]
	assert.Equal(t, body[:2000], ex.ResponseBody)
	assert.True(t, ex.ResponseBodyTruncated)
	assert.Equal(t, int64(4096), ex.ResponseSize)
}

func TestHistory_BodyWithinLimitIsStoredWhole(t *testing.T) {
	body := strings.Repeat("c", 3000)
	writer := executeForHistory(t, body,
		exec.WithMaxResponseSize(1024),
		exec.WithHistoryBodyLimit(3000),
	)

	ex := writer.executions[0]
	assert.Equal(t, body, ex.ResponseBody)
	assert.False(t, ex.ResponseBodyTruncated)
	assert.Equal(t, int64(3000), ex.ResponseSize)
}

func TestHistory_ZeroLimitStoresWholeBody(t *testing.T) {
	body := strings.Repeat("d", 500)
	writer := executeForHistory(t, body, exec.WithHistoryBodyLimit(0))

	assert.Equal(t, body, writer.executions[0].ResponseBody)
	assert.False(t, writer.executions[0].ResponseBodyTruncated)
}

func TestHistory_TruncationKeepsTextValidUTF8(t *testing.T) {
	// "é" is two bytes, so a cut at an odd byte count splits a character.
	body := strings.Repeat("é", 50)
	writer := executeForHistory(t, body, exec.WithHistoryBodyLimit(11))

	ex := writer.executions[0]
	assert.True(t, utf8.ValidString(ex.ResponseBody))
	assert.Equal(t, strings.Repeat("é", 5), ex.ResponseBody)
	assert.True(t, ex.ResponseBodyTruncated)
}

func TestHistory_DefaultLimitIsOneMiB(t *testing.T) {
	assert.Equal(t, 1<<20, exec.DefaultHistoryBodyLimit)
}
