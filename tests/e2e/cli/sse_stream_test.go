//go:build e2e

package cli

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// newEventStreamServer serves a text/event-stream response with the given
// status. The handler writes the body and may block to keep the stream open.
func newEventStreamServer(
	t *testing.T,
	status int,
	handler func(w http.ResponseWriter, r *http.Request),
) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startQuarkWithHome starts the quark binary without waiting for it, so a
// test can read stdout while the response is still open. The caller must
// drain stdout and call Wait.
func startQuarkWithHome(
	t *testing.T,
	home string,
	extraEnv []string,
	args ...string,
) (*exec.Cmd, io.ReadCloser, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = home
	cmd.Env = append(append(os.Environ(), "HOME="+home, "USERPROFILE="+home), extraEnv...)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	return cmd, stdout, &stderr
}

// tempDirEnv points every variable that os.TempDir consults at dir. Go reads
// TMPDIR on Unix but TMP and TEMP on Windows, so all three are set.
func tempDirEnv(dir string) []string {
	return []string{"TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir}
}

// exitCodeOf maps a Wait error to an exit code (0 when err is nil).
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "unexpected wait error: %v", err)
	return exitErr.ExitCode()
}

func TestCLI_RunStreamPrintsEachEventAsItArrives(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	release := make(chan struct{})
	var once sync.Once
	releaseStream := func() { once.Do(func() { close(release) }) }

	srv := newEventStreamServer(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: first\n\n")
		_ = http.NewResponseController(w).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "event: done\nid: 2\ndata: second\ndata: line\n\n")
	})
	// Registered after the server so it runs first and unblocks the handler.
	t.Cleanup(releaseStream)
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Events",
		Method: http.MethodGet,
		URL:    srv.URL + "/events",
	})

	cmd, stdout, stderr := startQuarkWithHome(t, home, nil, "run", "API/Events")
	reader := bufio.NewReader(stdout)
	first, err := reader.ReadString('\n')
	require.NoError(t, err, "the first event must be printed before the stream ends")
	assert.Equal(t, "first\n", first)

	releaseStream()
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	waitErr := cmd.Wait()
	require.Equal(t, 0, exitCodeOf(t, waitErr), "stderr: %s", stderr.String())

	assert.Equal(t, "second\nline\n", string(rest))
	assert.Contains(t, stderr.String(), "Status: 200 OK (streaming events; Ctrl+C to stop)")
	assert.Contains(t, stderr.String(), "Size:")
}

func TestCLI_RunStreamVerboseShowsEventTypeAndID(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEventStreamServer(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "event: tick\nid: 7\ndata: one\n\n")
		fmt.Fprint(w, "data: two\n\n")
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Verbose",
		Method: http.MethodGet,
		URL:    srv.URL + "/verbose",
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Verbose", "--verbose")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Equal(t, "one\ntwo\n", out)
	assert.Contains(t, errOut, "Request: GET "+srv.URL+"/verbose")
	assert.Contains(t, errOut, "Response: 200 OK")
	assert.Contains(t, errOut, "event: tick id: 7")
	// The ID carries over to the next event, which has no type of its own.
	assert.Contains(t, errOut, "event:  id: 7")
}

func TestCLI_RunStreamFailFlagReportsErrorStatus(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEventStreamServer(t, http.StatusInternalServerError, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "data: boom\n\n")
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Failing",
		Method: http.MethodGet,
		URL:    srv.URL + "/fail",
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Failing")
	require.Equal(t, 0, code, "without --fail an HTTP error status is not an error")
	assert.Equal(t, "boom\n", out)
	assert.Contains(t, errOut, "Status: 500 Internal Server Error")

	out, errOut, code = runQuarkWithHome(t, home, "run", "API/Failing", "--fail")
	assert.NotEqual(t, 0, code)
	assert.Equal(t, "boom\n", out)
	assert.Contains(t, errOut, "run: HTTP 500 Internal Server Error")
}

// largeEventStreamBody returns the body for a stream of count events, each
// about 1 KiB, so the total is above the 10 MiB in-memory threshold.
func largeEventStreamBody(count int) string {
	event := "data: " + strings.Repeat("x", 1023) + "\n\n"
	return strings.Repeat(event, count)
}

func TestCLI_RunStreamLargeBodySpillsToTempFileAndIsRemoved(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	const count = 12000
	body := largeEventStreamBody(count)
	srv := newEventStreamServer(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Large",
		Method: http.MethodGet,
		URL:    srv.URL + "/large",
	})

	tmp := t.TempDir()
	cmd, stdout, stderr := startQuarkWithHome(t, home, tempDirEnv(tmp), "run", "API/Large")
	out, readErr := io.ReadAll(stdout)
	require.NoError(t, readErr)
	require.Equal(t, 0, exitCodeOf(t, cmd.Wait()), "stderr: %s", stderr.String())

	assert.Equal(t, count, strings.Count(string(out), "\n"), "every event is printed")
	assert.Contains(t, stderr.String(), fmt.Sprintf("Size:   %d bytes", len(body)))
	leftover, err := filepath.Glob(filepath.Join(tmp, "quark-response-*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, leftover, "the spilled response file is removed after the run")
}

func TestCLI_RunStreamLargeBodyFailsWhenTempFileCannotBeCreated(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	body := largeEventStreamBody(12000)
	srv := newEventStreamServer(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:   "NoTmp",
		Method: http.MethodGet,
		URL:    srv.URL + "/large",
	})

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, errOut, code := runQuarkWithHomeEnv(t, home, tempDirEnv(missing), "run", "API/NoTmp")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, "create temp file")
}

func TestCLI_RunStreamIgnoresBOMCommentsRetryAndInvalidFields(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEventStreamServer(t, http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "\xef\xbb\xbfdata: bom\r\n\r\n")
		fmt.Fprint(w, ": keep-alive comment\n\n")
		fmt.Fprint(w, "retry: 3000\ndata: r\n\n")
		fmt.Fprint(w, "retry: soon\n")
		fmt.Fprint(w, "not-a-field\n")
		fmt.Fprint(w, "id: a\x00b\ndata: id-null\n\n")
		fmt.Fprint(w, "event: custom\ndata: multi\ndata: lines\n\n")
		fmt.Fprint(w, "data\n\n")
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Fields",
		Method: http.MethodGet,
		URL:    srv.URL + "/fields",
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Fields")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Equal(t, "bom\nr\nid-null\nmulti\nlines\n\n", out)
}

func TestCLI_RunStreamConnectionDropFailsTheRun(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		// One complete event (chunk of 11 bytes), then a chunk cut short.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/event-stream\r\n" +
			"Transfer-Encoding: chunked\r\n\r\n" +
			"b\r\ndata: one\n\n\r\n" +
			"5\r\ndata:")
		_ = buf.Flush()
	}))
	t.Cleanup(srv.Close)
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Drop",
		Method: http.MethodGet,
		URL:    srv.URL + "/drop",
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Drop")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, "read body")
	assert.Contains(t, out, "one")
}

func TestCLI_RunStreamInterruptEndsStreamAndPrintsSummary(t *testing.T) {
	if runtime.GOOS == "windows" {
		// os.Process.Signal(os.Interrupt) is not implemented on Windows, so a
		// test cannot interrupt a child process there.
		t.Skip("sending os.Interrupt to a child process is not supported on windows")
	}
	home, colID := setupHomeWithCollection(t)
	srv := newEventStreamServer(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: tick\n\n")
		_ = http.NewResponseController(w).Flush()
		// Stay open until the client stops reading.
		<-r.Context().Done()
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Interrupt",
		Method: http.MethodGet,
		URL:    srv.URL + "/open",
	})

	cmd, stdout, stderr := startQuarkWithHome(t, home, nil, "run", "API/Interrupt")
	// If an assertion fails early, kill the child so the server handler sees
	// the connection close. Otherwise httptest's Close waits forever.
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	reader := bufio.NewReader(stdout)
	first, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "tick\n", first)

	require.NoError(t, cmd.Process.Signal(os.Interrupt))
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	code := exitCodeOf(t, cmd.Wait())

	assert.Empty(t, rest)
	assert.Equal(t, 0, code, "stderr: %s", stderr.String())
	assert.Contains(t, stderr.String(), "Size:")
}
