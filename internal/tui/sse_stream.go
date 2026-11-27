package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// StreamingRequestExecutor is an optional extension of RequestExecutor that
// delivers text/event-stream responses event by event. *exec.Executor
// satisfies it; executors without it are used through Execute alone.
type StreamingRequestExecutor interface {
	ExecuteStream(
		ctx context.Context,
		req *domain.Request,
		h exec.StreamHandler,
	) (*exec.ExecuteResult, error)
}

// maxStreamBatch bounds how many queued stream items one message carries, so
// a fast stream re-renders per batch instead of per event.
const maxStreamBatch = 256

// streamItem is one thing a running request reports: its headers, an event,
// or the final httpResponseMsg/httpErrMsg (always the last item).
type streamItem struct {
	open  *exec.ExecuteResult
	event *exec.SSEEvent
	done  tea.Msg
}

// streamBatchMsg carries the stream items that were ready at once.
type streamBatchMsg struct {
	seq       int
	requestID string
	ch        <-chan streamItem
	items     []streamItem
}

// executeRequest runs req and returns its result message. With a streaming
// executor the request runs in a goroutine and the first batch of stream
// items is returned; the handler keeps reading until the final item.
func executeRequest(
	ctx context.Context,
	executor RequestExecutor,
	req *domain.Request,
	seq int,
) tea.Msg {
	streamer, ok := executor.(StreamingRequestExecutor)
	if !ok {
		result, err := executor.Execute(ctx, req)
		if err != nil {
			return httpErrMsg{requestID: req.ID, err: err}
		}
		return httpResponseMsg{requestID: req.ID, result: result}
	}
	// The handler drains the channel until it is closed, so sends never
	// block for long; the buffer only smooths bursts.
	ch := make(chan streamItem, maxStreamBatch)
	go func() {
		defer close(ch)
		result, err := streamer.ExecuteStream(ctx, req, exec.StreamHandler{
			OnOpen:  func(r *exec.ExecuteResult) { ch <- streamItem{open: r} },
			OnEvent: func(ev exec.SSEEvent) { ch <- streamItem{event: &ev} },
		})
		if err != nil {
			ch <- streamItem{done: httpErrMsg{requestID: req.ID, err: err}}
			return
		}
		ch <- streamItem{done: httpResponseMsg{requestID: req.ID, result: result}}
	}()
	return waitStreamCmd(ch, seq, req.ID)()
}

// waitStreamCmd waits for the next stream item and collects any others that
// are already queued.
func waitStreamCmd(ch <-chan streamItem, seq int, requestID string) tea.Cmd {
	return func() tea.Msg {
		first, ok := <-ch
		if !ok {
			return nil
		}
		msg := streamBatchMsg{seq: seq, requestID: requestID, ch: ch, items: []streamItem{first}}
		for len(msg.items) < maxStreamBatch && first.done == nil {
			select {
			case item, ok := <-ch:
				if !ok {
					return msg
				}
				msg.items = append(msg.items, item)
				if item.done != nil {
					return msg
				}
			default:
				return msg
			}
		}
		return msg
	}
}

// handleStreamBatch shows stream items in the response pane as they arrive.
// Items from a request that has since been replaced are drained and dropped.
func (m Model) handleStreamBatch(msg streamBatchMsg) (tea.Model, tea.Cmd) {
	current := msg.seq == m.streamSeq
	visible := current && (msg.requestID == "" || m.activeRequest == nil ||
		m.activeRequest.ID == msg.requestID)
	for _, item := range msg.items {
		switch {
		case item.done != nil:
			if !current {
				return m, nil
			}
			m = m.endStream()
			return m.Update(item.done)
		case !visible:
		case item.open != nil:
			m = m.beginStream(item.open)
		case item.event != nil && m.streaming:
			m = m.appendStreamEvent(*item.event)
		}
	}
	return m, waitStreamCmd(msg.ch, msg.seq, msg.requestID)
}

func (m Model) beginStream(opened *exec.ExecuteResult) Model {
	if m.response != nil {
		_ = m.response.Cleanup()
	}
	m.streaming = true
	m.streamEvents = 0
	m.streamBody = nil
	m.err = nil
	m.response = opened
	m.responseTab = bodyTab
	return m
}

// appendStreamEvent adds one event to the live body. A new result value is
// stored each time so the response pane notices the change.
func (m Model) appendStreamEvent(ev exec.SSEEvent) Model {
	m.streamEvents++
	m.streamBody = append(m.streamBody, formatSSEEvent(ev)...)
	next := *m.response
	next.Body = m.streamBody
	next.Size = int64(len(m.streamBody))
	m.response = &next
	// Keep the reader's scroll position while events arrive.
	m.responseText.preserveOffsetOnReformat = true
	return m
}

func (m Model) endStream() Model {
	m.streaming = false
	m.streamBody = nil
	m.responseText.preserveOffsetOnReformat = false
	return m
}

// formatSSEEvent renders an event in wire form, one block per event.
func formatSSEEvent(ev exec.SSEEvent) string {
	var sb strings.Builder
	if ev.Type != "" {
		sb.WriteString("event: " + ev.Type + "\n")
	}
	if ev.ID != "" {
		sb.WriteString("id: " + ev.ID + "\n")
	}
	for _, line := range strings.Split(ev.Data, "\n") {
		sb.WriteString("data: " + line + "\n")
	}
	sb.WriteString("\n")
	return sb.String()
}
