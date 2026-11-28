package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// wsEchoServer greets, echoes text, sends binary on "bin", and closes with
// 4000 on "bye".
func wsEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte("hello"))
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			switch string(data) {
			case "bye":
				_ = c.Close(4000, "bye")
				return
			case "bin":
				_ = c.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3})
			default:
				_ = c.Write(ctx, websocket.MessageText, []byte("echo: "+string(data)))
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsTestURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// wsTestModel returns a sized model and a real executor. Cleanup closes any
// session the test left open.
func wsTestModel(t *testing.T) (Model, *exec.Executor) {
	t.Helper()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	ex := exec.New(transport)
	m := New(Deps{Executor: ex, Ctx: context.Background()})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	sized := asModel(t, tm)
	return sized, ex
}

// openWS dispatches a send for url the way startRequest does and applies the
// handshake result to m.
func openWS(t *testing.T, m Model, ex *exec.Executor, url string) (Model, tea.Cmd) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.wsDialCtx = ctx
	msg := dispatchRequest(ctx, ex, &domain.Request{ID: "r1", Method: "GET", URL: url})
	require.IsType(t, wsOpenedMsg{}, msg)
	tm, cmd := m.Update(msg)
	opened := asModel(t, tm)
	t.Cleanup(func() {
		if opened.ws.session != nil {
			_ = opened.ws.session.Close(1000, "")
		}
	})
	return opened, cmd
}

// asModel returns the Model behind tm, failing the test if it is some other
// tea.Model.
func asModel(t *testing.T, tm tea.Model) Model {
	t.Helper()
	m, ok := tm.(Model)
	require.True(t, ok, "got %T", tm)
	return m
}

// step runs cmd and applies its message, returning the model and the next
// command.
func step(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	require.NotNil(t, cmd)
	tm, next := m.Update(cmd())
	return asModel(t, tm), next
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = asModel(t, tm)
	}
	return m
}

func key(t *testing.T, m Model, k tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	tm, cmd := m.Update(tea.KeyMsg{Type: k})
	return asModel(t, tm), cmd
}

func logTexts(m Model) []string {
	var out []string
	for _, line := range m.ws.log {
		out = append(out, line.dir+" "+line.text)
	}
	return out
}

func TestWebSocketSessionSendsReceivesAndCloses(t *testing.T) {
	srv := wsEchoServer(t)
	m, ex := wsTestModel(t)
	m, cmd := openWS(t, m, ex, wsTestURL(srv))

	require.Equal(t, wsMode, m.mode)
	assert.Equal(t, []string{"· connected: 101 Switching Protocols"}, logTexts(m))

	m, cmd = step(t, m, cmd) // greeting
	assert.Contains(t, logTexts(m), "← hello")

	m = typeText(t, m, "ping")
	m, sendCmd := key(t, m, tea.KeyEnter)
	assert.Contains(t, logTexts(m), "→ ping")
	assert.Equal(t, "", m.ws.input.Value(), "the input is cleared after sending")
	require.NotNil(t, sendCmd)
	assert.Nil(t, sendCmd(), "a successful send reports nothing")

	m, cmd = step(t, m, cmd) // echo
	assert.Contains(t, logTexts(m), "← echo: ping")

	m = typeText(t, m, "bin")
	m, sendCmd = key(t, m, tea.KeyEnter)
	require.NotNil(t, sendCmd)
	sendCmd()
	m, cmd = step(t, m, cmd)
	assert.Contains(t, logTexts(m), "← <binary, 3 bytes>")

	m = typeText(t, m, "bye")
	m, sendCmd = key(t, m, tea.KeyEnter)
	require.NotNil(t, sendCmd)
	sendCmd()
	m, closeCmd := step(t, m, cmd) // the peer's close frame
	assert.True(t, m.ws.closed)
	assert.Equal(t, wsMode, m.mode, "the modal stays open after the server closes")
	var closeLine string
	for _, line := range logTexts(m) {
		if strings.Contains(line, "4000") {
			closeLine = line
		}
	}
	assert.Contains(t, closeLine, "bye", "the close reason is shown")
	require.NotNil(t, closeCmd, "the session is released after the server closes")
	closeCmd()

	view := m.View()
	assert.Contains(t, view, "WebSocket")
	assert.Contains(t, view, "closed")
	assert.Contains(t, view, "> ", "the input line uses the open session's prompt")

	m = typeText(t, m, "after")
	m, sendCmd = key(t, m, tea.KeyEnter)
	assert.Nil(t, sendCmd, "sending after the close does nothing")
	assert.Equal(t, "after", m.ws.input.Value())

	m, escCmd := key(t, m, tea.KeyEsc)
	assert.Equal(t, normalMode, m.mode)
	assert.Equal(t, wsState{}, m.ws)
	require.NotNil(t, m.response, "the transcript becomes the response")
	assert.Equal(t, "101 Switching Protocols", m.response.Status)
	assert.Contains(t, string(m.response.Body), "echo: ping")
	assert.Equal(t, bodyTab, m.responseTab)
	assert.Equal(t, responsePane, m.focus)
	require.NotNil(t, escCmd)
	assert.Nil(t, escCmd(), "closing an already closed session is a no-op")
}

func TestWebSocketNewSessionClosesTheOpenOne(t *testing.T) {
	srv := wsEchoServer(t)
	m, ex := wsTestModel(t)
	m, cmd := openWS(t, m, ex, wsTestURL(srv))
	m, _ = step(t, m, cmd)
	first := m.ws.session

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.wsDialCtx = ctx
	msg := dispatchRequest(ctx, ex, &domain.Request{ID: "r2", Method: "GET", URL: wsTestURL(srv)})
	require.IsType(t, wsOpenedMsg{}, msg)
	tm, openCmd := m.Update(msg)
	m = asModel(t, tm)
	second := m.ws.session
	require.NotSame(t, first, second)
	t.Cleanup(func() { _ = second.Close(1000, "") })

	// The batch closes the replaced session, then waits on the new one. Only
	// the close is run here.
	batch, ok := openCmd().(tea.BatchMsg)
	require.True(t, ok)
	require.Len(t, batch, 2)
	assert.Nil(t, batch[0]())
	assert.ErrorIs(t, first.SendText(ctx, "late"), exec.ErrWebSocketClosed)
}

func TestWebSocketEscClosesLiveSession(t *testing.T) {
	srv := wsEchoServer(t)
	m, ex := wsTestModel(t)
	m, cmd := openWS(t, m, ex, wsTestURL(srv))
	m, _ = step(t, m, cmd) // greeting; the next read is pending

	m, closeCmd := key(t, m, tea.KeyEsc)
	assert.Equal(t, normalMode, m.mode)
	require.NotNil(t, closeCmd)
	assert.Nil(t, closeCmd(), "Close waits for the peer and returns without a message")
}

func TestWebSocketEscWithoutSessionReturnsToNormal(t *testing.T) {
	m, _ := wsTestModel(t)
	m.mode = wsMode
	m, cmd := key(t, m, tea.KeyEsc)
	assert.Equal(t, normalMode, m.mode)
	assert.Nil(t, cmd)
}

func TestWebSocketOpenFailureReturnsToNormal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	m, ex := wsTestModel(t)
	ctx := context.Background()
	m.wsDialCtx = ctx
	msg := dispatchRequest(ctx, ex, &domain.Request{ID: "r1", Method: "GET", URL: wsTestURL(srv)})
	errMsg, ok := msg.(httpErrMsg)
	require.True(t, ok, "got %T", msg)
	assert.Error(t, errMsg.err)

	tm, _ := m.Update(msg)
	assert.NotEqual(t, wsMode, asModel(t, tm).mode)
}

func TestWebSocketStaleHandshakeIsClosed(t *testing.T) {
	srv := wsEchoServer(t)
	m, ex := wsTestModel(t)
	stale, cancelStale := context.WithCancel(context.Background())
	defer cancelStale()
	current, cancelCurrent := context.WithCancel(context.Background())
	defer cancelCurrent()
	m.wsDialCtx = current

	msg := dispatchRequest(stale, ex, &domain.Request{ID: "r1", Method: "GET", URL: wsTestURL(srv)})
	require.IsType(t, wsOpenedMsg{}, msg)
	tm, cmd := m.Update(msg)
	assert.Equal(t, normalMode, asModel(t, tm).mode,
		"a superseded handshake must not open the modal")
	require.NotNil(t, cmd)
	assert.Nil(t, cmd(), "the superseded session is closed")
}

func TestWebSocketSendFailureIsLogged(t *testing.T) {
	srv := wsEchoServer(t)
	m, ex := wsTestModel(t)
	m, cmd := openWS(t, m, ex, wsTestURL(srv))
	m, _ = step(t, m, cmd)

	tm, _ := m.Update(wsSendFailedMsg{session: m.ws.session, err: errors.New("boom")})
	m = asModel(t, tm)
	assert.Contains(t, logTexts(m), "! send failed: boom")

	tm, _ = m.Update(wsSendFailedMsg{session: nil, err: errors.New("stale")})
	assert.NotContains(t, logTexts(asModel(t, tm)), "! send failed: stale")
}

func TestWebSocketIgnoresEventsFromOtherSessions(t *testing.T) {
	srv := wsEchoServer(t)
	m, ex := wsTestModel(t)
	m, cmd := openWS(t, m, ex, wsTestURL(srv))
	m, _ = step(t, m, cmd)
	before := len(m.ws.log)

	tm, next := m.Update(wsEventMsg{session: nil, msg: exec.WSMessage{Data: []byte("x")}})
	assert.Len(t, asModel(t, tm).ws.log, before)
	assert.Nil(t, next)
}

func TestWebSocketErrorEventMarksClosed(t *testing.T) {
	srv := wsEchoServer(t)
	m, ex := wsTestModel(t)
	m, cmd := openWS(t, m, ex, wsTestURL(srv))
	m, _ = step(t, m, cmd)

	tm, next := m.Update(wsEventMsg{
		session: m.ws.session,
		err:     errors.New("exec: websocket receive: broken pipe"),
	})
	m = asModel(t, tm)
	assert.True(t, m.ws.closed)
	assert.Contains(t, logTexts(m), "! exec: websocket receive: broken pipe")
	assert.NotNil(t, next, "the session is released")
	next()
}

func TestWebSocketScrollingClampsToLog(t *testing.T) {
	m, _ := wsTestModel(t)
	m.mode = wsMode
	m.ws.url = "ws://example.test/socket"
	for i := range 60 {
		m.ws.add(wsDirReceived, fmt.Sprintf("line %d", i), m.ws.started)
	}
	page := m.wsPageRows()
	require.Less(t, page, 60)

	m, _ = key(t, m, tea.KeyPgUp)
	assert.Equal(t, page, m.ws.scroll)
	assert.Contains(t, m.View(), "newer rows")

	for range 20 {
		m, _ = key(t, m, tea.KeyPgUp)
	}
	assert.Equal(t, 60-page, m.ws.scroll, "scrolling stops at the oldest row")

	m, _ = key(t, m, tea.KeyPgDown)
	assert.Equal(t, 60-2*page, m.ws.scroll)
	m, _ = key(t, m, tea.KeyUp)
	assert.Equal(t, 60-2*page+1, m.ws.scroll)
	m, _ = key(t, m, tea.KeyDown)
	assert.Equal(t, 60-2*page, m.ws.scroll)

	m, _ = key(t, m, tea.KeyEnd)
	assert.Equal(t, 0, m.ws.scroll)
	assert.NotContains(t, m.View(), "newer rows")
}

func TestWebSocketLogWrapsMultilineAndTruncates(t *testing.T) {
	m, _ := wsTestModel(t)
	m.ws.add(wsDirReceived, "first\nsecond", m.ws.started)
	m.ws.add(wsDirError, strings.Repeat("x", 500), m.ws.started)

	rows := m.wsRenderedLines(20)
	require.Len(t, rows, 3)
	assert.True(t, strings.HasSuffix(rows[0].text, "first"))
	assert.True(t, strings.HasSuffix(rows[1].text, "second"))
	assert.Equal(t, wsDirError, rows[2].dir)
	assert.LessOrEqual(t, len([]rune(rows[2].text)), 20)
}

func TestWebSocketLogIsBounded(t *testing.T) {
	m, _ := wsTestModel(t)
	for i := range wsLogLimit + 5 {
		m.ws.add(wsDirReceived, fmt.Sprint(i), m.ws.started)
	}
	require.Len(t, m.ws.log, wsLogLimit)
	assert.Equal(t, "5", m.ws.log[0].text, "the oldest entries are dropped")
}

// httpOnlyExecutor implements RequestExecutor and nothing else.
type httpOnlyExecutor struct {
	result *exec.ExecuteResult
	err    error
}

func (f httpOnlyExecutor) Execute(context.Context, *domain.Request) (*exec.ExecuteResult, error) {
	return f.result, f.err
}

func TestDispatchRequestUsesHTTPForNonWebSocketURLs(t *testing.T) {
	ctx := context.Background()
	msg := dispatchRequest(ctx, httpOnlyExecutor{result: &exec.ExecuteResult{StatusCode: 200}},
		&domain.Request{ID: "r1", URL: "https://example.test/"})
	require.IsType(t, httpResponseMsg{}, msg)

	msg = dispatchRequest(ctx, httpOnlyExecutor{err: errors.New("nope")},
		&domain.Request{ID: "r1", URL: "ws://example.test/"})
	require.IsType(t, httpErrMsg{}, msg,
		"an executor without WebSocket support gets the request as HTTP")
}

func TestWebSocketViewShowsStatusAndHints(t *testing.T) {
	m, _ := wsTestModel(t)
	m.mode = wsMode
	m.ws.url = "wss://example.test/live"
	m.ws.add(wsDirStatus, "connected: 101 Switching Protocols", m.ws.started)
	view := m.View()
	assert.Contains(t, view, "wss://example.test/live")
	assert.Contains(t, view, "connected")
	assert.Contains(t, view, "Enter send")
}
