//go:build e2e

package tui_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// wsLoop plays the Bubble Tea runtime for the WebSocket tests. Each command
// runs on its own goroutine and its message is delivered back to the model on
// the test goroutine, so a blocking receive does not stall the test.
type wsLoop struct {
	t    *testing.T
	m    tui.Model
	msgs chan tea.Msg
}

func newWSLoop(t *testing.T, m tui.Model) *wsLoop {
	return &wsLoop{t: t, m: m, msgs: make(chan tea.Msg, 4096)}
}

func (l *wsLoop) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		if msg := cmd(); msg != nil {
			l.msgs <- msg
		}
	}()
}

func (l *wsLoop) apply(msg tea.Msg) {
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, cmd := range batch {
			l.run(cmd)
		}
		return
	}
	m, cmd := callUpdateWithCmd(l.t, l.m, msg)
	l.m = m
	l.run(cmd)
}

// key sends one key press and starts whatever command it returns.
func (l *wsLoop) key(k tea.KeyMsg) {
	m, cmd := callUpdateWithCmd(l.t, l.m, k)
	l.m = m
	l.run(cmd)
}

// typeText types text into the focused input, one rune per key press.
func (l *wsLoop) typeText(text string) {
	for _, r := range text {
		l.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// until delivers messages until the model satisfies cond.
func (l *wsLoop) until(cond func(tui.Model) bool) {
	l.t.Helper()
	deadline := time.After(10 * time.Second)
	for !cond(l.m) {
		select {
		case msg := <-l.msgs:
			l.apply(msg)
		case <-deadline:
			require.FailNow(l.t, "timed out waiting for the WebSocket view", "view:\n%s", l.m.View())
		}
	}
}

// settle delivers messages that are already pending, without waiting.
func (l *wsLoop) settle() {
	for {
		select {
		case msg := <-l.msgs:
			l.apply(msg)
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}

// newWSModel returns a model whose request is seeded with body and targets
// the handler's server over ws://. The returned loop has already sent the
// request, so the caller only drives messages.
func newWSModel(
	t *testing.T,
	timeout time.Duration,
	body string,
	handler http.HandlerFunc,
) (*wsLoop, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	var opts []exec.Option
	if timeout > 0 {
		opts = append(opts, exec.WithTimeout(timeout))
	}

	col := &domain.Collection{ID: "col-ws", Name: "Sockets"}
	req := &domain.Request{ID: "req-ws", CollectionID: col.ID, Name: "Socket", Method: "GET", Body: body}
	st := setupStore(t, col)
	m := newE2EModel(t, st, exec.New(transport, opts...))
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if msg := runCmd(t, cmd); msg != nil {
		m = callUpdate(t, m, msg)
	}
	require.Equal(t, tui.RequestPane, m.Focus())

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/socket"
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	for _, r := range url {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	require.NotNil(t, cmd)

	loop := newWSLoop(t, m)
	loop.run(cmd)
	return loop, url
}

// wsEchoHandler echoes every message back until the client leaves.
func wsEchoHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

func TestE2E_WebSocketEchoRoundTripAndTranscript(t *testing.T) {
	loop, _ := newWSModel(t, 0, "", wsEchoHandler(t))
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "connected: 101") })
	assertViewContains(t, loop.m, "WebSocket")
	assertViewContains(t, loop.m, "Enter send")

	loop.typeText("ping")
	loop.key(tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, loop.m, "→ ping")
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "← ping") })

	// Esc closes the modal and leaves the transcript in the response pane.
	loop.key(tea.KeyMsg{Type: tea.KeyEsc})
	loop.settle()
	assertViewNotContains(t, loop.m, "Enter send")
	assert.Equal(t, tui.ResponsePane, loop.m.Focus())
	assertViewContains(t, loop.m, "connected: 101")
	assertViewContains(t, loop.m, "→ ping")
	assertViewContains(t, loop.m, "← ping")
}

func TestE2E_WebSocketEmptyInputIsNotSent(t *testing.T) {
	loop, _ := newWSModel(t, 0, "", wsEchoHandler(t))
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "connected: 101") })

	loop.key(tea.KeyMsg{Type: tea.KeyEnter})
	loop.settle()
	assertViewNotContains(t, loop.m, "→ ")
	loop.key(tea.KeyMsg{Type: tea.KeyEsc})
}

func TestE2E_WebSocketServerCloseEndsSession(t *testing.T) {
	loop, _ := newWSModel(t, 0, "", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	})
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "connected: 101") })

	loop.typeText("hello")
	loop.key(tea.KeyMsg{Type: tea.KeyEnter})
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "websocket closed: 1000 bye") })
	assertViewContains(t, loop.m, "closed")

	// Input after the peer has closed is ignored, not sent.
	loop.typeText("again")
	loop.key(tea.KeyMsg{Type: tea.KeyEnter})
	loop.settle()
	assertViewNotContains(t, loop.m, "→ again")

	loop.key(tea.KeyMsg{Type: tea.KeyEsc})
	assertViewContains(t, loop.m, "websocket closed: 1000 bye")
}

func TestE2E_WebSocketConnectionDropIsReported(t *testing.T) {
	loop, _ := newWSModel(t, 0, "", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Drop the TCP connection without a close frame.
		_ = conn.CloseNow()
	})
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "websocket receive") })
	assertViewContains(t, loop.m, "closed")
	loop.key(tea.KeyMsg{Type: tea.KeyEsc})
}

func TestE2E_WebSocketBinaryFramesAreSummarisedAndLogScrolls(t *testing.T) {
	loop, _ := newWSModel(t, 0, "", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		if err := conn.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
			return
		}
		for i := 1; i <= 30; i++ {
			if err := conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf("line-%02d", i))); err != nil {
				return
			}
		}
		<-ctx.Done()
	})
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "line-30") })
	assertViewNotContains(t, loop.m, "newer rows")

	// The log is taller than the modal: PgUp shows the older rows, which
	// include the binary frame, and End returns to the tail.
	loop.key(tea.KeyMsg{Type: tea.KeyPgUp})
	assertViewContains(t, loop.m, "<binary, 3 bytes>")
	assertViewContains(t, loop.m, "↓ 6 newer rows (End)")
	loop.key(tea.KeyMsg{Type: tea.KeyDown})
	assertViewContains(t, loop.m, "↓ 5 newer rows (End)")
	loop.key(tea.KeyMsg{Type: tea.KeyUp})
	loop.key(tea.KeyMsg{Type: tea.KeyUp})
	assertViewContains(t, loop.m, "↓ 6 newer rows (End)")
	loop.key(tea.KeyMsg{Type: tea.KeyPgDown})
	assertViewNotContains(t, loop.m, "newer rows")
	loop.key(tea.KeyMsg{Type: tea.KeyPgUp})
	loop.key(tea.KeyMsg{Type: tea.KeyEnd})
	assertViewNotContains(t, loop.m, "newer rows")

	loop.key(tea.KeyMsg{Type: tea.KeyEsc})
}

func TestE2E_WebSocketLogKeepsOnlyTheNewestEntries(t *testing.T) {
	loop, _ := newWSModel(t, 0, "", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		for i := 0; i < 1100; i++ {
			if err := conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf("line-%04d", i))); err != nil {
				return
			}
		}
		<-ctx.Done()
	})
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "line-1099") })

	// The 1000-entry cap drops the oldest entries, including the status line.
	loop.key(tea.KeyMsg{Type: tea.KeyEsc})
	assertViewNotContains(t, loop.m, "connected: 101")
	assertViewContains(t, loop.m, "line-0100")
}

func TestE2E_WebSocketHandshakeRejectionIsReported(t *testing.T) {
	loop, _ := newWSModel(t, 0, "", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "request failed") })
	assertViewContains(t, loop.m, "request failed")
	assertViewContains(t, loop.m, "403")
	assert.False(t, loop.m.Loading())
}

func TestE2E_WebSocketHandshakeTimeoutIsReported(t *testing.T) {
	loop, _ := newWSModel(t, 200*time.Millisecond, "", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	loop.until(func(m tui.Model) bool { return strings.Contains(m.View(), "Request timed out") })
	assertViewContains(t, loop.m, "Request timed out")
	assert.False(t, loop.m.Loading())
}

func TestE2E_WebSocketRejectsRequestBody(t *testing.T) {
	loop, _ := newWSModel(t, 0, `{"not":"allowed"}`, wsEchoHandler(t))
	loop.until(func(m tui.Model) bool {
		return strings.Contains(m.View(), "a WebSocket handshake cannot send a request body")
	})
	assertViewContains(t, loop.m, "a WebSocket handshake cannot send a request body")
	assert.False(t, loop.m.Loading())
}
