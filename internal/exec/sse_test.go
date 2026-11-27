package exec_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestIsEventStream(t *testing.T) {
	assert.True(t, exec.IsEventStream("text/event-stream"))
	assert.True(t, exec.IsEventStream("text/event-stream; charset=utf-8"))
	assert.True(t, exec.IsEventStream("Text/Event-Stream"))
	assert.False(t, exec.IsEventStream("text/plain"))
	assert.False(t, exec.IsEventStream(""))
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// sseServer writes each chunk, flushes, then waits for the next signal.
func sseServer(t *testing.T, chunks []string, step <-chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Stream", "yes")
		w.WriteHeader(http.StatusOK)
		flush(w)
		for _, chunk := range chunks {
			if step != nil {
				select {
				case <-step:
				case <-r.Context().Done():
					return
				}
			}
			fmt.Fprint(w, chunk)
			flush(w)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExecuteStream_ParsesEventsIncrementally(t *testing.T) {
	step := make(chan struct{})
	srv := sseServer(t, []string{
		"\xef\xbb\xbf: comment\nid: 1\ndata: first\n\n",
		"event: update\ndata: line one\r\ndata:line two\nretry: 1500\n\n",
		"data: no blank line at EOF",
	}, step)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	var mu sync.Mutex
	var opened *exec.ExecuteResult
	events := make(chan exec.SSEEvent, 4)
	e := exec.New(transport, exec.WithTimeout(time.Second))
	done := make(chan struct{})
	var result *exec.ExecuteResult
	var execErr error
	go func() {
		defer close(done)
		result, execErr = e.ExecuteStream(context.Background(),
			&domain.Request{Method: "GET", URL: srv.URL},
			exec.StreamHandler{
				OnOpen: func(r *exec.ExecuteResult) {
					mu.Lock()
					opened = r
					mu.Unlock()
				},
				OnEvent: func(ev exec.SSEEvent) { events <- ev },
			})
	}()

	step <- struct{}{}
	first := <-events
	assert.Equal(t, exec.SSEEvent{ID: "1", Data: "first"}, first)
	mu.Lock()
	require.NotNil(t, opened)
	assert.Equal(t, http.StatusOK, opened.StatusCode)
	assert.Equal(t, "yes", http.Header(opened.Headers).Get("X-Stream"))
	mu.Unlock()

	step <- struct{}{}
	second := <-events
	assert.Equal(t, exec.SSEEvent{
		Type: "update", ID: "1", Data: "line one\nline two", Retry: 1500,
	}, second)

	step <- struct{}{}
	<-done
	require.NoError(t, execErr)
	assert.Empty(t, events, "an unterminated event is not dispatched")
	assert.Contains(t, string(result.Body), "data: no blank line at EOF")
	assert.Equal(t, int64(len(result.Body)), result.Size)
}

func TestExecuteStream_StreamOutlivesTimeoutAndCancelEndsIt(t *testing.T) {
	step := make(chan struct{})
	srv := sseServer(t, []string{"data: a\n\n", "data: never\n\n"}, step)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	writer := &recordingExecutionWriter{}
	e := exec.New(transport,
		exec.WithTimeout(50*time.Millisecond),
		exec.WithExecutionWriter(writer))

	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan exec.SSEEvent, 2)
	done := make(chan struct{})
	var result *exec.ExecuteResult
	var execErr error
	go func() {
		defer close(done)
		result, execErr = e.ExecuteStream(ctx,
			&domain.Request{ID: "r1", Method: "GET", URL: srv.URL},
			exec.StreamHandler{OnEvent: func(ev exec.SSEEvent) { events <- ev }})
	}()
	step <- struct{}{}
	assert.Equal(t, "a", (<-events).Data)
	time.Sleep(150 * time.Millisecond) // past the request timeout
	cancel()
	<-done

	require.NoError(t, execErr, "cancelling an open stream ends it normally")
	assert.Equal(t, "data: a\n\n", string(result.Body))
	require.Len(t, writer.executions, 1)
	assert.Equal(t, "data: a\n\n", writer.executions[0].ResponseBody)
}

func TestExecuteStream_TimeoutBeforeHeaders(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := exec.New(transport, exec.WithTimeout(30*time.Millisecond))
	_, err := e.ExecuteStream(context.Background(),
		&domain.Request{Method: "GET", URL: srv.URL},
		exec.StreamHandler{OnEvent: func(exec.SSEEvent) {}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, exec.ErrTimeout), "got %v", err)
	assert.False(t, errors.Is(err, exec.ErrRequestCancelled))
}

func TestExecuteStream_CancelBeforeHeaders(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(30*time.Millisecond, cancel)
	e := exec.New(transport, exec.WithTimeout(time.Minute))
	_, err := e.ExecuteStream(ctx,
		&domain.Request{Method: "GET", URL: srv.URL},
		exec.StreamHandler{OnEvent: func(exec.SSEEvent) {}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, exec.ErrRequestCancelled), "got %v", err)
	assert.False(t, errors.Is(err, exec.ErrTimeout))
}

func TestExecuteStream_NonEventStreamIsBuffered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "data: x\n\n")
	}))
	defer srv.Close()
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	called := false
	e := exec.New(transport)
	result, err := e.ExecuteStream(context.Background(),
		&domain.Request{Method: "GET", URL: srv.URL},
		exec.StreamHandler{
			OnOpen:  func(*exec.ExecuteResult) { called = true },
			OnEvent: func(exec.SSEEvent) { called = true },
		})
	require.NoError(t, err)
	assert.False(t, called)
	assert.Equal(t, "data: x\n\n", string(result.Body))
}

func TestExecute_EventStreamWithoutHandlerIsBuffered(t *testing.T) {
	srv := sseServer(t, []string{"data: x\n\n"}, nil)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	result, err := exec.New(transport).Execute(context.Background(),
		&domain.Request{Method: "GET", URL: srv.URL})
	require.NoError(t, err)
	assert.Equal(t, "data: x\n\n", string(result.Body))
}

func TestExecuteStream_LargeStreamSpillsToTempFile(t *testing.T) {
	payload := strings.Repeat("data: 0123456789\n\n", 20)
	srv := sseServer(t, []string{payload}, nil)
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	count := 0
	e := exec.New(transport, exec.WithMaxResponseSize(64))
	result, err := e.ExecuteStream(context.Background(),
		&domain.Request{Method: "GET", URL: srv.URL},
		exec.StreamHandler{OnEvent: func(exec.SSEEvent) { count++ }})
	require.NoError(t, err)
	defer func() { _ = result.Cleanup() }()
	assert.Equal(t, 20, count)
	require.NotEmpty(t, result.TempPath)
	assert.Nil(t, result.Body)
	data, err := os.ReadFile(result.TempPath)
	require.NoError(t, err)
	assert.Equal(t, payload, string(data))
	assert.Equal(t, int64(len(payload)), result.Size)
}
