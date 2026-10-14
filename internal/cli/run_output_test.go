package cli

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestWriteResponseBodyPrettyPrintsJSONAndPreservesPlainText(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeResponseBody(&out, []byte(`{"ok":true,"items":[1,2]}`), "application/json", false))
	require.Equal(t, "{\n  \"ok\": true,\n  \"items\": [\n    1,\n    2\n  ]\n}\n", out.String())

	out.Reset()
	require.NoError(t, writeResponseBody(&out, []byte("not json"), "text/plain", false))
	require.Equal(t, "not json\n", out.String())
}

func TestWriteResponseBodyTerminalHandlesBinaryAndHighlightsJSON(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeResponseBody(&out, []byte{0, 1, 2}, "application/octet-stream", true))
	require.Contains(t, out.String(), "binary response body omitted")
	require.NotContains(t, out.String(), string([]byte{0, 1, 2}))

	out.Reset()
	require.NoError(t, writeResponseBody(&out, []byte(`{"ok":true}`), "application/json", true))
	require.Contains(t, out.String(), "ok")
	require.Contains(t, out.String(), "true")
	require.NotEqual(t, "{\n  \"ok\": true\n}\n", out.String())
}

func TestPrintRunResultVerboseIncludesRequestAndResponseDetails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	req := &domain.Request{
		Method: "POST", URL: "https://example.test", Headers: `{"Content-Type":["application/json"]}`,
		Body: `{"send":true}`, AuthType: "bearer", AuthConfig: `{"token":"secret"}`,
	}
	result := &exec.ExecuteResult{
		Status: "200 OK", Size: 12, Body: []byte(`{"ok":true}`),
		Headers: map[string][]string{"Z-Last": {"z"}, "A-First": {"a"}},
	}
	require.NoError(t, printRunResult(cmd, req, result, true))
	require.Contains(t, stdout.String(), "Status: 200 OK")
	require.Contains(t, stdout.String(), "\"ok\": true")
	require.Contains(t, stderr.String(), "Request: POST https://example.test")
	require.Contains(t, stderr.String(), "Request auth config: {\"token\":\"secret\"}")
	require.Contains(t, stderr.String(), "Request body (13 bytes)")
	require.Less(t, strings.Index(stderr.String(), "< A-First:"), strings.Index(stderr.String(), "< Z-Last:"))
}

func TestPrintRunResultStreamsBodyAndCleansTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "response.txt")
	require.NoError(t, os.WriteFile(path, []byte("streamed response"), 0600))
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	result := &exec.ExecuteResult{Status: "200 OK", Size: 17, TempPath: path, Headers: http.Header{"Content-Type": {"text/plain"}}}
	require.NoError(t, printRunResult(cmd, &domain.Request{}, result, false))
	require.Contains(t, stdout.String(), "streamed response")
	require.NoFileExists(t, path)
	require.Empty(t, result.TempPath)
}

func TestPrintRunResultHandlesEmptyJSONStreamAndReadError(t *testing.T) {
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	require.NoError(t, printRunResult(cmd, &domain.Request{}, &exec.ExecuteResult{Status: "204 No Content"}, false))
	require.Contains(t, stdout.String(), "Status: 204 No Content")
	require.NotContains(t, stdout.String(), "{}")

	path := filepath.Join(t.TempDir(), "response.json")
	body := []byte(`{"ok":true}`)
	require.NoError(t, os.WriteFile(path, body, 0600))
	stdout.Reset()
	result := &exec.ExecuteResult{
		Status: "200 OK", Size: int64(len(body)), TempPath: path,
		Headers: http.Header{"Content-Type": {"application/problem+json"}},
	}
	require.NoError(t, printRunResult(cmd, &domain.Request{}, result, false))
	require.Contains(t, stdout.String(), `"ok": true`)
	require.NoFileExists(t, path)

	result = &exec.ExecuteResult{Status: "200 OK", Size: 1, TempPath: filepath.Join(t.TempDir(), "missing")}
	require.ErrorContains(t, printRunResult(cmd, &domain.Request{}, result, false), "read response body")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrPermission }

func TestWriteResponseBodyHandlesInvalidJSONAndWriterError(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeResponseBody(&out, []byte("{bad"), "application/json", false))
	require.Equal(t, "{bad\n", out.String())
	require.NoError(t, writeResponseBody(&out, nil, "text/plain", false))
	require.ErrorIs(t, writeResponseBody(failingWriter{}, []byte("text"), "text/plain", false), os.ErrPermission)
}

func TestBinaryResponseClassification(t *testing.T) {
	require.True(t, binaryResponse([]byte{0, 1}, ""))
	require.True(t, binaryResponse([]byte("bytes"), "application/octet-stream"))
	require.False(t, binaryResponse([]byte("plain"), "text/plain"))
	require.False(t, binaryResponse([]byte("{}"), "application/json"))
}
