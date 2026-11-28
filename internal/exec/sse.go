package exec

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// eventStreamMediaType is the Content-Type of a Server-Sent Events response.
const eventStreamMediaType = "text/event-stream"

// SSEEvent is one dispatched Server-Sent Event.
type SSEEvent struct {
	// Type is the "event:" field. Empty means the default "message" type.
	Type string
	// ID is the last "id:" field seen on the stream (it carries over between
	// events, as in the EventSource specification).
	ID string
	// Data is the event's "data:" lines joined with "\n".
	Data string
	// Retry is the "retry:" reconnection time in milliseconds, or 0.
	Retry int
}

// StreamHandler receives a text/event-stream response while it is read.
// Both callbacks are optional and run on the goroutine that called
// ExecuteStream. Responses with any other Content-Type are read in full, as
// Execute does, and never reach the handler.
type StreamHandler struct {
	// OnOpen is called once, when the response headers arrive. The result has
	// StatusCode, Status and Headers set; the body is not read yet.
	OnOpen func(*ExecuteResult)
	// OnEvent is called for each complete event as it arrives.
	OnEvent func(SSEEvent)
}

func (h StreamHandler) enabled() bool { return h.OnOpen != nil || h.OnEvent != nil }

// IsEventStream reports whether a Content-Type value is text/event-stream.
func IsEventStream(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.EqualFold(strings.TrimSpace(contentType), eventStreamMediaType)
	}
	return mediaType == eventStreamMediaType
}

// readEventStream reads a text/event-stream body incrementally, dispatching
// each event to h as it completes. The raw bytes are kept as the result body
// (spilled to a temp file past maxResponseSize) so history stores the stream.
//
// A read error after the caller's context is done ends the stream normally:
// cancelling is how a user stops an open-ended stream.
func (e *Executor) readEventStream(
	resp *http.Response,
	elapsed time.Duration,
	h StreamHandler,
	stopped func() bool,
) (*ExecuteResult, error) {
	result := &ExecuteResult{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    map[string][]string(resp.Header),
		Duration:   elapsed,
	}
	if h.OnOpen != nil {
		h.OnOpen(&ExecuteResult{
			StatusCode: result.StatusCode,
			Status:     result.Status,
			Headers:    result.Headers,
			Duration:   elapsed,
		})
	}

	body := &spillBuffer{limit: e.maxResponseSize}
	parser := sseParser{}
	reader := bufio.NewReader(io.TeeReader(resp.Body, body))
	var readErr error
	for {
		line, err := reader.ReadString('\n')
		if line != "" && (err == nil || errors.Is(err, io.EOF)) {
			if event, ok := parser.line(line); ok && h.OnEvent != nil {
				h.OnEvent(event)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !stopped() {
				readErr = err
			}
			break
		}
	}
	if body.err != nil {
		body.discard()
		return nil, fmt.Errorf("exec: write temp: %w", body.err)
	}
	if readErr != nil {
		body.discard()
		return nil, fmt.Errorf("exec: read body: %w", readErr)
	}
	if err := body.finish(result); err != nil {
		return nil, err
	}
	return result, nil
}

// sseParser implements the line handling of the EventSource specification.
type sseParser struct {
	started   bool
	eventType string
	lastID    string
	retry     int
	data      strings.Builder
	hasData   bool
}

// line consumes one line (with its terminator) and returns an event when the
// line is the blank line that dispatches one.
func (p *sseParser) line(raw string) (SSEEvent, bool) {
	line := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
	if !p.started {
		p.started = true
		line = strings.TrimPrefix(line, "\xef\xbb\xbf")
	}
	if line == "" {
		return p.dispatch()
	}
	if strings.HasPrefix(line, ":") {
		return SSEEvent{}, false
	}
	field, value, found := strings.Cut(line, ":")
	if found {
		value = strings.TrimPrefix(value, " ")
	}
	switch field {
	case "event":
		p.eventType = value
	case "data":
		if p.hasData {
			p.data.WriteByte('\n')
		}
		p.data.WriteString(value)
		p.hasData = true
	case "id":
		if !strings.ContainsRune(value, 0) {
			p.lastID = value
		}
	case "retry":
		if n, err := strconv.Atoi(value); err == nil && n >= 0 {
			p.retry = n
		}
	}
	return SSEEvent{}, false
}

func (p *sseParser) dispatch() (SSEEvent, bool) {
	defer func() {
		p.eventType = ""
		p.data.Reset()
		p.hasData = false
		p.retry = 0
	}()
	if !p.hasData {
		return SSEEvent{}, false
	}
	return SSEEvent{
		Type:  p.eventType,
		ID:    p.lastID,
		Data:  p.data.String(),
		Retry: p.retry,
	}, true
}

// spillBuffer keeps up to limit bytes in memory and moves everything to a
// temp file once that is exceeded, like readResponse does for large bodies.
type spillBuffer struct {
	limit int
	mem   bytes.Buffer
	file  *os.File
	size  int64
	err   error
}

func (b *spillBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	b.size += int64(len(p))
	if b.file == nil && b.mem.Len()+len(p) <= b.limit {
		return b.mem.Write(p)
	}
	if b.file == nil {
		f, err := newScratchFile()
		if err != nil {
			b.err = err
			return 0, err
		}
		b.file = f
		if _, err := f.Write(b.mem.Bytes()); err != nil {
			b.err = err
			return 0, err
		}
		b.mem = bytes.Buffer{}
	}
	if _, err := b.file.Write(p); err != nil {
		b.err = err
		return 0, err
	}
	return len(p), nil
}

func (b *spillBuffer) finish(result *ExecuteResult) error {
	result.Size = b.size
	if b.file == nil {
		result.Body = b.mem.Bytes()
		return nil
	}
	name := b.file.Name()
	if err := b.file.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("exec: close temp: %w", err)
	}
	result.TempPath = name
	return nil
}

func (b *spillBuffer) discard() {
	if b.file != nil {
		name := b.file.Name()
		_ = b.file.Close()
		_ = os.Remove(name)
	}
}
