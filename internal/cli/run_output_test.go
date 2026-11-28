package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestWriteResponseBodyPrettyPrintsJSONAndPreservesPlainText(t *testing.T) {
	var out bytes.Buffer
	require.NoError(
		t,
		writeResponseBody(&out, []byte(`{"ok":true,"items":[1,2]}`), "application/json", false),
	)
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
		Method:     "POST",
		URL:        "https://example.test",
		Headers:    `{"Content-Type":["application/json"]}`,
		Body:       `{"send":true}`,
		AuthType:   "bearer",
		AuthConfig: `{"token":"secret"}`,
	}
	result := &exec.ExecuteResult{
		Status: "200 OK", Size: 12, Body: []byte(`{"ok":true}`),
		Headers: map[string][]string{"Z-Last": {"z"}, "A-First": {"a"}},
	}
	require.NoError(t, printRunResult(cmd, req, result, true))
	require.Contains(t, stderr.String(), "Status: 200 OK")
	require.NotContains(
		t,
		stdout.String(),
		"Status:",
		"status belongs on stderr so stdout is the body",
	)
	require.Contains(t, stdout.String(), "\"ok\": true")
	require.Contains(t, stderr.String(), "Request: POST https://example.test")
	require.Contains(t, stderr.String(), "Request auth config: {\"token\":\"secret\"}")
	require.Contains(t, stderr.String(), "Request body (13 bytes)")
	require.Less(
		t,
		strings.Index(stderr.String(), "< A-First:"),
		strings.Index(stderr.String(), "< Z-Last:"),
	)
}

func TestPrintRunResultStreamsBodyAndCleansTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "response.txt")
	require.NoError(t, os.WriteFile(path, []byte("streamed response"), 0600))
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	result := &exec.ExecuteResult{
		Status:   "200 OK",
		Size:     17,
		TempPath: path,
		Headers:  http.Header{"Content-Type": {"text/plain"}},
	}
	require.NoError(t, printRunResult(cmd, &domain.Request{}, result, false))
	require.Contains(t, stdout.String(), "streamed response")
	require.NoFileExists(t, path)
	require.Empty(t, result.TempPath)
}

func TestPrintRunResultHandlesEmptyJSONStreamAndReadError(t *testing.T) {
	var stdout bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	require.NoError(
		t,
		printRunResult(
			cmd,
			&domain.Request{},
			&exec.ExecuteResult{Status: "204 No Content"},
			false,
		),
	)
	require.NotContains(t, stdout.String(), "Status:")
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

	result = &exec.ExecuteResult{
		Status:   "200 OK",
		Size:     1,
		TempPath: filepath.Join(t.TempDir(), "missing"),
	}
	require.ErrorContains(
		t,
		printRunResult(cmd, &domain.Request{}, result, false),
		"read response body",
	)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrPermission }

func TestWriteResponseBodyHandlesInvalidJSONAndWriterError(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeResponseBody(&out, []byte("{bad"), "application/json", false))
	require.Equal(t, "{bad\n", out.String())
	require.NoError(t, writeResponseBody(&out, nil, "text/plain", false))
	require.ErrorIs(
		t,
		writeResponseBody(failingWriter{}, []byte("text"), "text/plain", false),
		os.ErrPermission,
	)
}

func TestBinaryResponseClassification(t *testing.T) {
	require.True(t, binaryResponse([]byte{0, 1}, ""))
	require.True(t, binaryResponse([]byte("bytes"), "application/octet-stream"))
	require.False(t, binaryResponse([]byte("plain"), "text/plain"))
	require.False(t, binaryResponse([]byte("{}"), "application/json"))
}

// Piping `quark run` must give exactly the response body on stdout.
func TestRunOutput_StdoutIsExactlyTheBody(t *testing.T) {
	body := []byte("plain text body")
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	result := &exec.ExecuteResult{
		Status: "200 OK", Size: int64(len(body)), Body: body,
		Headers: map[string][]string{"Content-Type": {"text/plain"}},
	}
	require.NoError(t, printRunResult(cmd, &domain.Request{}, result, false))
	assert.Equal(t, string(body), strings.TrimSuffix(stdout.String(), "\n"),
		"stdout must hold the body and nothing else (a single trailing newline is allowed)")
	assert.Contains(t, stderr.String(), "Status: 200 OK")
}

// runOutputStore is a one-request store for the -o and -i tests.
func runOutputStore() *fakeRunStore {
	const collectionID = "col-1"
	defaultEnv := &domain.Environment{
		ID: "env-default", CollectionID: collectionID, Name: "default", Data: "{}",
	}
	return &fakeRunStore{
		collections: []*domain.Collection{{ID: collectionID, Name: "API"}},
		requests: map[string][]*domain.Request{
			collectionID: {{
				ID: "req-1", CollectionID: collectionID, Name: "ping", Method: "GET",
				URL: "https://example.test/ping",
			}},
		},
		globalEnv: &domain.Environment{ID: "global", Name: "Global", Data: "{}"},
		envsByID:  map[string]*domain.Environment{"env-default": defaultEnv},
		envsByCol: map[string][]*domain.Environment{collectionID: {defaultEnv}},
	}
}

func runOutputTransport(contentType, body string) *recordingRoundTripper {
	header := http.Header{}
	header.Set("Content-Type", contentType)
	header.Set("X-Trace", "abc")
	return &recordingRoundTripper{response: &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     header,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}}
}

func TestNewRunCmd_OutputFlagWritesBodyToFile(t *testing.T) {
	transport := runOutputTransport("application/json", `{"ok":  true}`)
	dest := filepath.Join(t.TempDir(), "body.json")

	var stdout, stderr bytes.Buffer
	cmd := NewRunCmd(runOutputStore(), exec.New(transport))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"API/ping", "-o", dest})
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, `{"ok":  true}`, string(data), "the body is saved as received, not re-indented")
	assert.Empty(t, stdout.String(), "stdout carries nothing when the body goes to a file")
	assert.Contains(t, stderr.String(), "Status: 200 OK")
}

func TestNewRunCmd_IncludePrintsStatusAndHeadersOnStdout(t *testing.T) {
	transport := runOutputTransport("text/plain", "pong")

	var stdout, stderr bytes.Buffer
	cmd := NewRunCmd(runOutputStore(), exec.New(transport))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"API/ping", "--include"})
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "200 OK\nContent-Type: text/plain\nX-Trace: abc\n\npong\n", stdout.String())
	assert.Contains(t, stderr.String(), "Status: 200 OK")
	assert.NotContains(t, stdout.String(), "Status:", "the status block stays on stderr")
}

func TestWriteRunBodyFileCopiesStreamedBody(t *testing.T) {
	src := filepath.Join(t.TempDir(), "streamed")
	require.NoError(t, os.WriteFile(src, []byte("streamed body"), 0o600))
	dest := filepath.Join(t.TempDir(), "out.txt")

	require.NoError(t, writeRunBodyFile(dest, &exec.ExecuteResult{TempPath: src}))
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "streamed body", string(data))

	require.ErrorContains(t,
		writeRunBodyFile(dest, &exec.ExecuteResult{TempPath: filepath.Join(t.TempDir(), "gone")}),
		"read response body")
}
