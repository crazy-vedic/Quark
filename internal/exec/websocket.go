package exec

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ErrWebSocketClosed is returned by WSSession.Receive once the connection is
// closed, by either side. A close frame from the peer adds its code and reason
// to the message.
var ErrWebSocketClosed = errors.New("websocket closed")

// maxWebSocketMessage bounds one incoming message. The library default (32 KiB)
// is too small for normal API traffic; a frame above this limit closes the
// connection.
const maxWebSocketMessage = 16 << 20

// IsWebSocketURL reports whether a request URL uses the ws or wss scheme.
func IsWebSocketURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "ws" || parsed.Scheme == "wss"
}

// WSMessage is one data message received from the server.
type WSMessage struct {
	Binary bool
	Data   []byte
	At     time.Time
}

// WSSession is an open WebSocket connection. Receive is for one goroutine at a
// time; Send and Close may be called from any goroutine, including while
// Receive is blocked.
type WSSession struct {
	conn    *websocket.Conn
	cancel  context.CancelFunc
	ctx     context.Context
	status  string
	headers http.Header
	closed  atomic.Bool
	once    sync.Once
}

// Status is the handshake status line, such as "101 Switching Protocols".
func (s *WSSession) Status() string { return s.status }

// Headers are the handshake response headers.
func (s *WSSession) Headers() http.Header { return s.headers }

// OpenWebSocket performs the handshake for a ws:// or wss:// request. Stored
// headers, auth and client certificates apply exactly as they do for HTTP,
// because the handshake goes through the executor's transport. The request
// must have no body; the handshake is always a GET.
//
// The handshake is bounded by the executor's timeout. The returned session
// lives until Close, or until ctx is cancelled.
func (e *Executor) OpenWebSocket(ctx context.Context, req *domain.Request) (*WSSession, error) {
	startedAt := time.Now()
	record := func(snapshot *domain.Request, result *ExecuteResult, execErr error) {
		e.recordExecution(ctx, snapshot, result, execErr, startedAt, time.Now())
	}
	prepared, err := e.prepare(ctx, req)
	if err != nil {
		record(prepared, nil, err)
		return nil, err
	}
	req = prepared
	if !IsWebSocketURL(req.URL) {
		err := fmt.Errorf("%w: scheme must be ws or wss for a WebSocket request", ErrInvalidURL)
		record(req, nil, err)
		return nil, err
	}
	if strings.TrimSpace(req.Body) != "" {
		err := errors.New("exec: a WebSocket handshake cannot send a request body")
		record(req, nil, err)
		return nil, err
	}

	// Reuse the HTTP request builder for headers, Host and auth. Its scheme
	// check accepts http(s) only, so hand it the equivalent HTTP URL.
	handshake := *req
	handshake.URL = httpEquivalentURL(req.URL)
	httpReq, err := buildHTTPRequest(ctx, &handshake)
	if err != nil {
		wrapped := fmt.Errorf("exec: build request: %w", err)
		record(req, nil, wrapped)
		return nil, wrapped
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	dialCtx, cancelDial := sessionCtx, context.CancelFunc(func() {})
	if e.timeout > 0 {
		dialCtx, cancelDial = context.WithTimeout(sessionCtx, e.timeout)
	}
	defer cancelDial()

	dialStart := time.Now()
	conn, resp, err := websocket.Dial(dialCtx, req.URL, &websocket.DialOptions{
		HTTPClient: e.client,
		HTTPHeader: httpReq.Header,
		Host:       httpReq.Host,
	})
	elapsed := time.Since(dialStart)
	if err != nil {
		cancel()
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		classified := e.classifyError(err)
		if errors.Is(dialCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			classified = fmt.Errorf("exec: %w: %w", ErrTimeout, err)
		}
		record(req, nil, classified)
		return nil, classified
	}
	conn.SetReadLimit(maxWebSocketMessage)

	session := &WSSession{
		conn:    conn,
		cancel:  cancel,
		ctx:     sessionCtx,
		status:  resp.Status,
		headers: resp.Header.Clone(),
	}
	record(req, &ExecuteResult{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    map[string][]string(resp.Header),
		Duration:   elapsed,
	}, nil)
	return session, nil
}

// httpEquivalentURL maps ws to http and wss to https. Anything else is returned
// unchanged so buildHTTPRequest rejects it.
func httpEquivalentURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	}
	return parsed.String()
}

// SendText writes one text message. It blocks while the peer applies
// backpressure; ctx bounds the wait.
func (s *WSSession) SendText(ctx context.Context, text string) error {
	if err := s.conn.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		if s.closed.Load() {
			return fmt.Errorf("%w", ErrWebSocketClosed)
		}
		return fmt.Errorf("exec: websocket send: %w", err)
	}
	return nil
}

// Receive waits for the next data message. Control frames are handled by the
// library. Once the connection is closed it returns an error that wraps
// ErrWebSocketClosed: a peer close frame adds its code and reason, a local
// Close does not. Any other error is a broken connection.
func (s *WSSession) Receive() (WSMessage, error) {
	typ, data, err := s.conn.Read(s.ctx)
	if err != nil {
		return WSMessage{}, s.classifyReadErr(err)
	}
	return WSMessage{
		Binary: typ == websocket.MessageBinary,
		Data:   data,
		At:     time.Now(),
	}, nil
}

func (s *WSSession) classifyReadErr(err error) error {
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		if closeErr.Reason == "" {
			return fmt.Errorf("%w: %d", ErrWebSocketClosed, closeErr.Code)
		}
		return fmt.Errorf("%w: %d %s", ErrWebSocketClosed, closeErr.Code, closeErr.Reason)
	}
	if s.closed.Load() {
		return fmt.Errorf("%w", ErrWebSocketClosed)
	}
	return fmt.Errorf("exec: websocket receive: %w", err)
}

// Close sends a close frame with code and reason, waits briefly for the peer's
// reply, then releases the connection. It may block for up to the library's
// close timeout, so call it off the UI goroutine. Later calls return nil.
func (s *WSSession) Close(code int, reason string) error {
	var err error
	s.once.Do(func() {
		s.closed.Store(true)
		err = s.conn.Close(websocket.StatusCode(code), reason)
		// Cancel only after the close handshake: cancelling first would abort it.
		s.cancel()
	})
	return err
}
