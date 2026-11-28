package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// WebSocketOpener is an optional extension of RequestExecutor for ws:// and
// wss:// requests. *exec.Executor satisfies it.
type WebSocketOpener interface {
	OpenWebSocket(ctx context.Context, req *domain.Request) (*exec.WSSession, error)
}

// wsLogLimit bounds the message log so a chatty server cannot grow memory
// without limit. The oldest entries are dropped first.
const wsLogLimit = 1000

// wsSendTimeout bounds one send. A write that stalls this long ends the
// connection, as the library requires after a context expires mid-write.
const wsSendTimeout = 30 * time.Second

// Log directions. Status and error lines are not messages in either direction.
const (
	wsDirSent     = "→"
	wsDirReceived = "←"
	wsDirStatus   = "·"
	wsDirError    = "!"
)

// wsOpenedMsg reports a completed handshake. ctx identifies the send that
// started it, so a result from a superseded send can be recognised and closed.
type wsOpenedMsg struct {
	ctx       context.Context
	requestID string
	url       string
	session   *exec.WSSession
}

// wsEventMsg is one item from the server: a message, or the error that ended
// the session. Each event re-arms the wait, so the log updates as messages come.
type wsEventMsg struct {
	session *exec.WSSession
	msg     exec.WSMessage
	err     error
}

// wsSendFailedMsg reports a write that did not reach the server.
type wsSendFailedMsg struct {
	session *exec.WSSession
	err     error
}

type wsLogLine struct {
	at   time.Time
	dir  string
	text string
}

// wsState is the open WebSocket modal. The zero value means no session.
type wsState struct {
	session   *exec.WSSession
	url       string
	cancel    context.CancelFunc // cancels the handshake context; nil once open
	input     textinput.Model
	selection textedit.Selection
	log       []wsLogLine
	closed    bool
	started   time.Time
	// scroll is how many rendered rows the view sits above the newest entry.
	// Zero follows the tail.
	scroll int
}

func (s *wsState) add(dir, text string, at time.Time) {
	s.log = append(s.log, wsLogLine{at: at, dir: dir, text: text})
	if over := len(s.log) - wsLogLimit; over > 0 {
		s.log = append([]wsLogLine(nil), s.log[over:]...)
	}
}

// dispatchRequest runs req. A ws or wss URL opens a WebSocket session;
// anything else is executed as an HTTP request.
func dispatchRequest(ctx context.Context, executor RequestExecutor, req *domain.Request) tea.Msg {
	if opener, ok := executor.(WebSocketOpener); ok && exec.IsWebSocketURL(req.URL) {
		session, err := opener.OpenWebSocket(ctx, req)
		if err != nil {
			return httpErrMsg{requestID: req.ID, err: err}
		}
		return wsOpenedMsg{ctx: ctx, requestID: req.ID, url: req.URL, session: session}
	}
	result, err := executor.Execute(ctx, req)
	if err != nil {
		return httpErrMsg{requestID: req.ID, err: err}
	}
	return httpResponseMsg{requestID: req.ID, result: result}
}

func waitWSCmd(session *exec.WSSession) tea.Cmd {
	return func() tea.Msg {
		msg, err := session.Receive()
		return wsEventMsg{session: session, msg: msg, err: err}
	}
}

// closeWSCmd closes a session off the UI goroutine, since Close can wait for
// the peer's close reply.
func closeWSCmd(session *exec.WSSession, cancel context.CancelFunc) tea.Cmd {
	return func() tea.Msg {
		_ = session.Close(1000, "")
		if cancel != nil {
			cancel()
		}
		return nil
	}
}

func (m Model) handleWSOpened(msg wsOpenedMsg) (tea.Model, tea.Cmd) {
	if m.wsDialCtx == nil || msg.ctx != m.wsDialCtx {
		// A newer send superseded this one while it was connecting.
		return m, closeWSCmd(msg.session, nil)
	}
	m.wsDialCtx = nil
	m.loading = false
	m.err = nil
	m = m.clearStatus()
	// A session already open would be replaced, so it is closed first.
	var closePrev tea.Cmd
	if m.ws.session != nil {
		closePrev = closeWSCmd(m.ws.session, m.ws.cancel)
	}
	now := time.Now()
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 4096
	input.Width = max(1, m.wsInnerWidth()-2)
	input.Focus()
	m.ws = wsState{
		session: msg.session,
		url:     msg.url,
		cancel:  m.cancel,
		input:   input,
		started: now,
	}
	m.cancel = nil
	m.styleInputs()
	m.ws.add(wsDirStatus, "connected: "+msg.session.Status(), now)
	m.mode = wsMode
	return m, tea.Batch(closePrev, waitWSCmd(msg.session))
}

func (m Model) handleWSEvent(msg wsEventMsg) (tea.Model, tea.Cmd) {
	if m.mode != wsMode || m.ws.session != msg.session {
		return m, nil
	}
	if msg.err != nil {
		m.ws.closed = true
		dir := wsDirError
		if errors.Is(msg.err, exec.ErrWebSocketClosed) {
			dir = wsDirStatus
		}
		m.ws.add(dir, msg.err.Error(), time.Now())
		return m, closeWSCmd(msg.session, m.ws.cancel)
	}
	text := string(msg.msg.Data)
	if msg.msg.Binary {
		text = fmt.Sprintf("<binary, %d bytes>", len(msg.msg.Data))
	}
	at := msg.msg.At
	if at.IsZero() {
		at = time.Now()
	}
	m.ws.add(wsDirReceived, text, at)
	return m, waitWSCmd(msg.session)
}

func (m Model) handleWSSendFailed(msg wsSendFailedMsg) (tea.Model, tea.Cmd) {
	if m.mode != wsMode || m.ws.session != msg.session {
		return m, nil
	}
	if errors.Is(msg.err, exec.ErrWebSocketClosed) {
		m.ws.closed = true
		m.ws.add(wsDirStatus, msg.err.Error(), time.Now())
		return m, nil
	}
	m.ws.add(wsDirError, "send failed: "+msg.err.Error(), time.Now())
	return m, nil
}

func (m Model) handleWSKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.closeWSModal()
	case tea.KeyEnter:
		return m.sendWSInput()
	case tea.KeyPgUp:
		m = m.scrollWS(m.wsPageRows())
	case tea.KeyPgDown:
		m = m.scrollWS(-m.wsPageRows())
	case tea.KeyUp:
		m = m.scrollWS(1)
	case tea.KeyDown:
		m = m.scrollWS(-1)
	case tea.KeyEnd:
		m.ws.scroll = 0
	}
	// The shared editor handles text, selection and Ctrl+V, so paste behaves
	// the same as in the other single-line inputs.
	ed := m.newTextEditor()
	if cmd, handled := ed.HandleKey(&m.ws.selection, textedit.Input(&m.ws.input), msg); handled {
		return m, cmd
	}
	return m, nil
}

func (m Model) sendWSInput() (tea.Model, tea.Cmd) {
	text := m.ws.input.Value()
	if m.ws.session == nil || m.ws.closed || text == "" {
		return m, nil
	}
	m.ws.input.SetValue("")
	m.ws.add(wsDirSent, text, time.Now())
	m.ws.scroll = 0
	session := m.ws.session
	ctx, cancel := context.WithTimeout(m.ctx, wsSendTimeout)
	return m, func() tea.Msg {
		defer cancel()
		if err := session.SendText(ctx, text); err != nil {
			return wsSendFailedMsg{session: session, err: err}
		}
		return nil
	}
}

// closeWSModal ends the session and returns to normal mode. The transcript
// stays in the response pane so it can be read or copied afterwards.
func (m Model) closeWSModal() (tea.Model, tea.Cmd) {
	session, cancel := m.ws.session, m.ws.cancel
	if session != nil {
		m.setWSTranscriptResponse()
	}
	m.mode = normalMode
	m.ws = wsState{}
	if session == nil {
		return m, nil
	}
	return m, closeWSCmd(session, cancel)
}

func (m *Model) setWSTranscriptResponse() {
	var sb strings.Builder
	for _, line := range m.ws.log {
		sb.WriteString(line.at.Format("15:04:05.000"))
		sb.WriteString(" ")
		sb.WriteString(line.dir)
		sb.WriteString(" ")
		sb.WriteString(line.text)
		sb.WriteString("\n")
	}
	body := []byte(sb.String())
	if m.response != nil {
		_ = m.response.Cleanup()
	}
	status := m.ws.session.Status()
	m.response = &exec.ExecuteResult{
		StatusCode: 101,
		Status:     status,
		Headers:    m.ws.session.Headers(),
		Body:       body,
		Size:       int64(len(body)),
		Duration:   time.Since(m.ws.started),
	}
	m.responseTab = bodyTab
	m.focus = responsePane
}

// wsPageRows is how many log rows the modal shows at once.
func (m Model) wsPageRows() int {
	return max(3, m.height-14)
}

// wsRow is one drawn line of the log.
type wsRow struct {
	text string
	dir  string
}

// wsRenderedLines is the log as it is drawn: one row per line of text.
func (m Model) wsRenderedLines(width int) []wsRow {
	var rows []wsRow
	for _, entry := range m.ws.log {
		prefix := entry.at.Format("15:04:05") + " " + entry.dir + " "
		indent := strings.Repeat(" ", len(prefix))
		for i, line := range strings.Split(entry.text, "\n") {
			lead := indent
			if i == 0 {
				lead = prefix
			}
			rows = append(rows, wsRow{
				text: ansi.Truncate(lead+line, width, "…"),
				dir:  entry.dir,
			})
		}
	}
	return rows
}

// scrollWS moves the view by delta rows (positive is further back in time),
// clamped to the log.
func (m Model) scrollWS(delta int) Model {
	inner := m.wsInnerWidth()
	limit := max(0, len(m.wsRenderedLines(inner))-m.wsPageRows())
	m.ws.scroll = min(max(0, m.ws.scroll+delta), limit)
	return m
}

func (m Model) wsInnerWidth() int {
	return max(20, min(m.width-4, 110)) - 6
}

func (m Model) viewWSModal() string {
	width := max(20, min(m.width-4, 110))
	inner := m.wsInnerWidth()
	m.ws.input.Width = max(1, inner-2)

	state := "connected"
	if m.ws.closed {
		state = "closed"
	}
	var sb strings.Builder
	sb.WriteString(m.styles.titleStyle.Render(ansi.Truncate("WebSocket  "+m.ws.url, inner, "…")))
	sb.WriteString("\n")
	sb.WriteString(m.styles.mutedStyle.Render(state) + "\n\n")

	rows := m.wsRenderedLines(inner)
	page := m.wsPageRows()
	end := len(rows) - m.ws.scroll
	if end < 0 {
		end = 0
	}
	start := max(0, end-page)
	for _, row := range rows[start:end] {
		sb.WriteString(m.wsStyleRow(row) + "\n")
	}
	for i := end - start; i < page; i++ {
		sb.WriteString("\n")
	}
	if below := len(rows) - end; below > 0 {
		sb.WriteString(m.styles.mutedStyle.Render(fmt.Sprintf("↓ %d newer rows (End)", below)))
	}
	sb.WriteString("\n")
	sb.WriteString(m.ws.input.View())
	sb.WriteString("\n")
	sb.WriteString(m.styles.mutedStyle.Render(
		"Enter send · PgUp/PgDn scroll · End latest · Esc close",
	))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.accent).
		Padding(1, 2).
		Width(width)
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box.Render(sb.String()),
	)
}

// wsStyleRow colours a log row by its direction marker.
func (m Model) wsStyleRow(row wsRow) string {
	switch row.dir {
	case wsDirError:
		return m.styles.errorStyle.Render(row.text)
	case wsDirStatus:
		return m.styles.mutedStyle.Render(row.text)
	default:
		return row.text
	}
}
