package tui

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// streamFakeExecutor implements both RequestExecutor and StreamingRequestExecutor.
type streamFakeExecutor struct {
	events []exec.SSEEvent
	open   *exec.ExecuteResult
	result *exec.ExecuteResult
	err    error
}

func (f *streamFakeExecutor) Execute(
	context.Context,
	*domain.Request,
) (*exec.ExecuteResult, error) {
	return f.result, f.err
}

func (f *streamFakeExecutor) ExecuteStream(
	_ context.Context,
	_ *domain.Request,
	h exec.StreamHandler,
) (*exec.ExecuteResult, error) {
	if h.OnOpen != nil && f.open != nil {
		h.OnOpen(f.open)
	}
	for _, ev := range f.events {
		if h.OnEvent != nil {
			h.OnEvent(ev)
		}
	}
	return f.result, f.err
}

// bufferedExecutor implements only RequestExecutor, so requests go through Execute.
type bufferedExecutor struct {
	result *exec.ExecuteResult
	err    error
}

func (b bufferedExecutor) Execute(context.Context, *domain.Request) (*exec.ExecuteResult, error) {
	return b.result, b.err
}

func streamingTestModel(seq int, reqID string) Model {
	m := New(Deps{})
	m.streamSeq = seq
	m.activeRequest = &domain.Request{ID: reqID, Name: "events"}
	return m
}

func TestFormatSSEEvent(t *testing.T) {
	assert.Equal(t, "data: plain\n\n", formatSSEEvent(exec.SSEEvent{Data: "plain"}))
	assert.Equal(t,
		"event: tick\nid: 9\ndata: a\ndata: b\n\n",
		formatSSEEvent(exec.SSEEvent{Type: "tick", ID: "9", Data: "a\nb"}))
	assert.Equal(t, "data: \n\n", formatSSEEvent(exec.SSEEvent{}))
}

func TestExecuteRequest_BufferedExecutorReturnsResponse(t *testing.T) {
	result := &exec.ExecuteResult{StatusCode: http.StatusOK}
	msg := executeRequest(context.Background(), bufferedExecutor{result: result},
		&domain.Request{ID: "r1"}, 1)
	resp, ok := msg.(httpResponseMsg)
	require.True(t, ok, "got %T", msg)
	assert.Same(t, result, resp.result)
}

func TestExecuteRequest_BufferedExecutorErrorReturnsErr(t *testing.T) {
	msg := executeRequest(context.Background(), bufferedExecutor{err: errors.New("boom")},
		&domain.Request{ID: "r1"}, 1)
	require.IsType(t, httpErrMsg{}, msg)
}

func TestExecuteRequest_StreamingExecutorDeliversItemsInOrder(t *testing.T) {
	opened := &exec.ExecuteResult{StatusCode: http.StatusOK, Status: "200 OK"}
	final := &exec.ExecuteResult{StatusCode: http.StatusOK, Status: "200 OK", Size: 4}
	ex := &streamFakeExecutor{
		open:   opened,
		events: []exec.SSEEvent{{Data: "one"}, {Data: "two"}},
		result: final,
	}
	msg := executeRequest(context.Background(), ex, &domain.Request{ID: "r1"}, 3)
	batch, ok := msg.(streamBatchMsg)
	require.True(t, ok, "got %T", msg)
	require.Equal(t, 3, batch.seq)

	var got []string
	done := false
	for step := 0; step < 10 && !done; step++ {
		for _, item := range batch.items {
			switch {
			case item.open != nil:
				got = append(got, "open")
			case item.event != nil:
				got = append(got, "event:"+item.event.Data)
			case item.done != nil:
				got = append(got, "done")
				done = true
			}
		}
		if done {
			break
		}
		next := waitStreamCmd(batch.ch, batch.seq, batch.requestID)()
		nextBatch, ok := next.(streamBatchMsg)
		require.True(t, ok, "got %T", next)
		batch = nextBatch
	}
	assert.Equal(t, []string{"open", "event:one", "event:two", "done"}, got)
}

func TestHandleStreamBatch_ShowsEventsAsTheyArrive(t *testing.T) {
	m := streamingTestModel(1, "r1")
	ch := make(chan streamItem)
	close(ch)
	opened := &exec.ExecuteResult{StatusCode: http.StatusOK, Status: "200 OK"}

	tm, cmd := m.handleStreamBatch(streamBatchMsg{
		seq: 1, requestID: "r1", ch: ch,
		items: []streamItem{
			{open: opened},
			{event: &exec.SSEEvent{Type: "tick", Data: "hello"}},
			{event: &exec.SSEEvent{Data: "world"}},
		},
	})
	m, ok := tm.(Model)
	require.True(t, ok)
	assert.True(t, m.streaming)
	assert.Equal(t, 2, m.streamEvents)
	require.NotNil(t, m.response)
	assert.Equal(t, "event: tick\ndata: hello\n\ndata: world\n\n", string(m.response.Body))
	assert.Equal(t, int64(len(m.response.Body)), m.response.Size)
	assert.Equal(t, bodyTab, m.responseTab)
	assert.NotNil(t, cmd, "the handler keeps waiting for more items")
	assert.Nil(t, cmd(), "a closed channel ends the wait without a message")
}

func TestHandleStreamBatch_IgnoresEventsOfReplacedRequest(t *testing.T) {
	m := streamingTestModel(2, "r1")
	ch := make(chan streamItem)
	close(ch)

	tm, _ := m.handleStreamBatch(streamBatchMsg{
		seq: 1, requestID: "r1", ch: ch,
		items: []streamItem{{open: &exec.ExecuteResult{}}, {event: &exec.SSEEvent{Data: "stale"}}},
	})
	m, ok := tm.(Model)
	require.True(t, ok)
	assert.False(t, m.streaming)
	assert.Nil(t, m.response)
	assert.Equal(t, 0, m.streamEvents)
}

func TestHandleStreamBatch_IgnoresOtherRequestsEvents(t *testing.T) {
	m := streamingTestModel(1, "r2")
	ch := make(chan streamItem)
	close(ch)

	tm, _ := m.handleStreamBatch(streamBatchMsg{
		seq: 1, requestID: "r1", ch: ch,
		items: []streamItem{{open: &exec.ExecuteResult{}}, {event: &exec.SSEEvent{Data: "other"}}},
	})
	m, ok := tm.(Model)
	require.True(t, ok)
	assert.False(t, m.streaming)
	assert.Nil(t, m.response)
}

func TestHandleStreamBatch_EventWithoutOpenIsDropped(t *testing.T) {
	m := streamingTestModel(1, "r1")
	ch := make(chan streamItem)
	close(ch)

	tm, _ := m.handleStreamBatch(streamBatchMsg{
		seq: 1, requestID: "r1", ch: ch,
		items: []streamItem{{event: &exec.SSEEvent{Data: "orphan"}}},
	})
	m, ok := tm.(Model)
	require.True(t, ok)
	assert.False(t, m.streaming)
	assert.Equal(t, 0, m.streamEvents)
}

func TestHandleStreamBatch_DoneFinishesStreamWithFinalResult(t *testing.T) {
	m := streamingTestModel(1, "r1")
	m = m.beginStream(&exec.ExecuteResult{StatusCode: http.StatusOK, Status: "200 OK"})
	m = m.appendStreamEvent(exec.SSEEvent{Data: "x"})
	ch := make(chan streamItem)
	close(ch)
	final := &exec.ExecuteResult{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       []byte("data: x\n\n"),
	}

	tm, _ := m.handleStreamBatch(streamBatchMsg{
		seq: 1, requestID: "r1", ch: ch,
		items: []streamItem{{done: httpResponseMsg{requestID: "r1", result: final}}},
	})
	m, ok := tm.(Model)
	require.True(t, ok)
	assert.False(t, m.streaming)
	assert.False(t, m.loading)
	assert.Same(t, final, m.response)
}

func TestHandleStreamBatch_DoneWithErrorFinishesStream(t *testing.T) {
	m := streamingTestModel(1, "r1")
	m = m.beginStream(&exec.ExecuteResult{StatusCode: http.StatusOK})
	m.loading = true
	ch := make(chan streamItem)
	close(ch)

	tm, _ := m.handleStreamBatch(streamBatchMsg{
		seq: 1, requestID: "r1", ch: ch,
		items: []streamItem{{done: httpErrMsg{requestID: "r1", err: errors.New("broken")}}},
	})
	m, ok := tm.(Model)
	require.True(t, ok)
	assert.False(t, m.streaming)
	assert.False(t, m.loading)
	assert.Empty(t, m.streamBody)
}

func TestHandleStreamBatch_DoneFromReplacedRequestIsDropped(t *testing.T) {
	m := streamingTestModel(2, "r1")
	m = m.beginStream(&exec.ExecuteResult{StatusCode: http.StatusOK})
	m.loading = true
	ch := make(chan streamItem)
	close(ch)

	tm, cmd := m.handleStreamBatch(streamBatchMsg{
		seq: 1, requestID: "r1", ch: ch,
		items: []streamItem{{done: httpResponseMsg{
			requestID: "r1", result: &exec.ExecuteResult{},
		}}},
	})
	m, ok := tm.(Model)
	require.True(t, ok)
	assert.Nil(t, cmd)
	assert.True(t, m.streaming, "a stale request must not end the current stream")
	assert.True(t, m.loading)
}

func TestEndStreamClearsLiveState(t *testing.T) {
	m := streamingTestModel(1, "r1")
	m = m.beginStream(&exec.ExecuteResult{StatusCode: http.StatusOK})
	m = m.appendStreamEvent(exec.SSEEvent{Data: "x"})
	m = m.endStream()
	assert.False(t, m.streaming)
	assert.Nil(t, m.streamBody)
	assert.False(t, m.responseText.preserveOffsetOnReformat)
}

func TestLiveStreamReplacesHistoryAndShowsStatus(t *testing.T) {
	m := streamingTestModel(1, "r1")
	m.executions = []*domain.Execution{{ID: "old", StatusCode: 200}}
	m.execCursor = 0
	require.NotNil(t, m.selectedExecution())

	m = m.beginStream(&exec.ExecuteResult{StatusCode: http.StatusOK, Status: "200 OK"})
	m = m.appendStreamEvent(exec.SSEEvent{Data: "x"})
	assert.Nil(t, m.selectedExecution(), "history is hidden while a stream is live")

	status := m.viewLiveResponseStatus()
	assert.Contains(t, status, "200 OK")
	assert.Contains(t, status, "streaming")
	assert.Contains(t, status, "1 events")
	assert.Contains(t, status, "bytes")
}

func TestLiveResponseStatusWithoutStream(t *testing.T) {
	m := streamingTestModel(1, "r1")
	m.response = &exec.ExecuteResult{StatusCode: http.StatusOK, Status: "200 OK", Size: 5}
	status := m.viewLiveResponseStatus()
	assert.Contains(t, status, "200 OK")
	assert.NotContains(t, status, "streaming")
	assert.Contains(t, status, "5 bytes")
}
