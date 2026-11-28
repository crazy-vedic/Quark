//go:build e2e

package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// terminalSession runs the quark TUI under util-linux script(1), which gives
// it a pseudo-terminal, and drives it by writing key presses to its stdin.
type terminalSession struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *syncBuffer
	done  chan error
}

// syncBuffer is a bytes.Buffer that the process output goroutine writes and
// the test reads while the program is still running.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startTerminalSession starts `quark <args>` on a 120x40 pseudo-terminal.
func startTerminalSession(t *testing.T, home string, args ...string) *terminalSession {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs util-linux script(1) to allocate a pseudo-terminal")
	}
	scriptPath, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script(1) not available")
	}
	command := "stty cols 120 rows 40; " + binaryPath
	for _, arg := range args {
		command += " " + arg
	}
	cmd := exec.Command(scriptPath, "-qefc", command, "/dev/null")
	cmd.Dir = home
	// TERM=dumb stops the CLI waiting about 5s for the pty to answer a
	// terminal colour query.
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "TERM=dumb")
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	s := &terminalSession{t: t, cmd: cmd, stdin: stdin, out: &syncBuffer{}, done: make(chan error, 1)}
	cmd.Stdout = s.out
	cmd.Stderr = s.out
	require.NoError(t, cmd.Start())
	go func() { s.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})
	return s
}

// send writes raw key input to the terminal.
func (s *terminalSession) send(keys string) {
	s.t.Helper()
	_, err := io.WriteString(s.stdin, keys)
	require.NoError(s.t, err)
}

// waitFor polls the terminal output until it contains want.
func (s *terminalSession) waitFor(want string) {
	s.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(s.out.String(), want) {
		if time.Now().After(deadline) {
			require.FailNowf(s.t, "terminal output never showed the expected text",
				"want %q in:\n%s", want, s.out.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// exit waits for the program to end and returns its exit code.
func (s *terminalSession) exit() int {
	s.t.Helper()
	select {
	case err := <-s.done:
		if err == nil {
			return 0
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		require.NoError(s.t, err)
	case <-time.After(15 * time.Second):
		_ = s.cmd.Process.Kill()
		require.FailNow(s.t, "quark did not exit after quit", s.out.String())
	}
	return -1
}

// echoSocketServer echoes every text message back to the client.
func echoSocketServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			typ, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), typ, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// wsURL returns the ws:// address of a test server.
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/socket"
}

// openSeededSocket starts the TUI with one request named Socket selected and
// its send key pressed, so the caller only waits for the outcome.
func openSeededSocket(t *testing.T, s *terminalSession) {
	t.Helper()
	s.waitFor("Socket")
	s.send("j")
	s.send("\r")
	s.send("s")
}

// leaveModal presses Esc and then q. A lone Esc is read as Esc only after a
// short pause; without it the following "q" would arrive as Alt+q.
func leaveModal(s *terminalSession) {
	s.send("\x1b")
	time.Sleep(300 * time.Millisecond)
	s.send("q")
}

func TestCLI_TUIWebSocketSessionOnTerminal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := echoSocketServer(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Socket",
		Method: http.MethodGet,
		URL:    wsURL(srv),
	})

	s := startTerminalSession(t, home, "tui")
	openSeededSocket(t, s)
	s.waitFor("connected: 101")
	s.send("ping\r")
	s.waitFor("← ping")
	leaveModal(s)
	require.Equal(t, 0, s.exit(), "output: %s", s.out.String())
}

func TestCLI_TUIWebSocketServerCloseOnTerminal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}))
	t.Cleanup(srv.Close)
	seedRequest(t, home, colID, &domain.Request{Name: "Socket", Method: http.MethodGet, URL: wsURL(srv)})

	s := startTerminalSession(t, home, "tui")
	openSeededSocket(t, s)
	s.waitFor("connected: 101")
	s.send("hello\r")
	s.waitFor("websocket closed: 1000 bye")
	leaveModal(s)
	require.Equal(t, 0, s.exit(), "output: %s", s.out.String())
}

func TestCLI_TUIWebSocketConnectionDropOnTerminal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.CloseNow()
	}))
	t.Cleanup(srv.Close)
	seedRequest(t, home, colID, &domain.Request{Name: "Socket", Method: http.MethodGet, URL: wsURL(srv)})

	s := startTerminalSession(t, home, "tui")
	openSeededSocket(t, s)
	s.waitFor("websocket receive")
	leaveModal(s)
	require.Equal(t, 0, s.exit(), "output: %s", s.out.String())
}

func TestCLI_TUIWebSocketHandshakeRejectedOnTerminal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	seedRequest(t, home, colID, &domain.Request{Name: "Socket", Method: http.MethodGet, URL: wsURL(srv)})

	s := startTerminalSession(t, home, "tui")
	openSeededSocket(t, s)
	s.waitFor("request failed")
	s.waitFor("403")
	s.send("q")
	require.Equal(t, 0, s.exit(), "output: %s", s.out.String())
}

func TestCLI_TUIWebSocketRejectsRequestBodyOnTerminal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Socket", Method: http.MethodGet, URL: "ws://127.0.0.1:9/socket", Body: "{}",
	})

	s := startTerminalSession(t, home, "tui")
	openSeededSocket(t, s)
	s.waitFor("cannot send a request body")
	s.send("q")
	require.Equal(t, 0, s.exit(), "output: %s", s.out.String())
}

func TestCLI_TUIWebSocketUnresolvedVariableOnTerminal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Socket", Method: http.MethodGet, URL: "ws://127.0.0.1:9/{{missing_host}}",
	})

	s := startTerminalSession(t, home, "tui")
	openSeededSocket(t, s)
	s.waitFor("unresolved variable")
	s.send("q")
	require.Equal(t, 0, s.exit(), "output: %s", s.out.String())
}
