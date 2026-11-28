//go:build e2e

package tui_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// sendTypedURL types url into the request URL field and sends the request,
// returning the model and the command that performs the request.
func sendTypedURL(t *testing.T, m tui.Model, url string) (tui.Model, tea.Cmd) {
	t.Helper()
	require.Equal(t, tui.RequestPane, m.Focus())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	for _, r := range url {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	require.True(t, m.Loading(), "sending must put the request in flight")
	require.NotNil(t, cmd)
	return m, cmd
}

// stepStream runs one pending command and feeds its message back into the
// model. It returns the next command, or nil when the stream has finished.
func stepStream(t *testing.T, m tui.Model, cmd tea.Cmd) (tui.Model, tea.Cmd) {
	t.Helper()
	msg := runCmd(t, cmd)
	if msg == nil {
		return m, nil
	}
	return callUpdateWithCmd(t, m, msg)
}

// drainStream runs commands until none is left, delivering the whole stream.
func drainStream(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	t.Helper()
	for cmd != nil {
		m, cmd = stepStream(t, m, cmd)
	}
	return m
}

// newStreamingModel returns a model wired to a real executor and the server
// the test configures. The request is sent to the returned server's URL.
func newStreamingModel(t *testing.T, timeout time.Duration, handler http.HandlerFunc) (tui.Model, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	opts := []exec.Option{}
	if timeout > 0 {
		opts = append(opts, exec.WithTimeout(timeout))
	}
	col := &domain.Collection{ID: "col-1", Name: "Live"}
	req := &domain.Request{ID: "req-stream", CollectionID: col.ID, Name: "Stream", Method: "GET"}
	st := setupStore(t, col)
	m := newE2EModel(t, st, exec.New(transport, opts...))
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))

	// Select the request from the sidebar and move focus to its pane.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if msg := runCmd(t, cmd); msg != nil {
		m = callUpdate(t, m, msg)
	}
	require.Equal(t, tui.RequestPane, m.Focus())
	return m, srv
}

func TestE2E_StreamingResponseShowsEventsAsTheyArrive(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	releaseStream := func() { once.Do(func() { close(release) }) }

	m, srv := newStreamingModel(t, 0, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: tick\nid: 1\ndata: first\n\n")
		_ = http.NewResponseController(w).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: second\n\n")
	})
	t.Cleanup(releaseStream)

	m, cmd := sendTypedURL(t, m, srv.URL+"/stream")

	// Deliver until the first event is on screen, while the stream is open.
	for !strings.Contains(m.View(), "1 events") && cmd != nil {
		m, cmd = stepStream(t, m, cmd)
	}
	require.NotNil(t, cmd, "the stream ended before the first event was shown")
	assert.True(t, m.Loading(), "the stream is still open")
	assertViewContains(t, m, "Streaming events")
	assertViewContains(t, m, "data: first")
	assertViewNotContains(t, m, "data: second")

	releaseStream()
	m = drainStream(t, m, cmd)

	assert.False(t, m.Loading())
	assertViewNotContains(t, m, "Streaming events")
	assertViewContains(t, m, "200 OK")
	assertViewContains(t, m, "data: first")
	assertViewContains(t, m, "data: second")
}

func TestE2E_StreamingResponseEscStopsAndKeepsReceivedEvents(t *testing.T) {
	m, srv := newStreamingModel(t, 0, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: kept\n\n")
		_ = http.NewResponseController(w).Flush()
		// Never ends on its own; only the client's cancel stops it.
		<-r.Context().Done()
	})

	m, cmd := sendTypedURL(t, m, srv.URL+"/open")
	for !strings.Contains(m.View(), "1 events") && cmd != nil {
		m, cmd = stepStream(t, m, cmd)
	}
	require.NotNil(t, cmd)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m.Loading(), "Esc cancels the in-flight request")

	m = drainStream(t, m, cmd)
	assert.False(t, m.Loading())
	assertViewNotContains(t, m, "Streaming events")
	assertViewContains(t, m, "data: kept")
	assertViewContains(t, m, "200 OK")
}

func TestE2E_StreamingResponseLiveStatusCountsEvents(t *testing.T) {
	m, srv := newStreamingModel(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(w, "data: item-%d\n\n", i)
		}
	})

	m, cmd := sendTypedURL(t, m, srv.URL+"/count")
	m = drainStream(t, m, cmd)

	assert.False(t, m.Loading())
	assertViewContains(t, m, "data: item-1")
	assertViewContains(t, m, "data: item-3")
}

func TestE2E_RequestTimeoutBeforeHeadersIsReported(t *testing.T) {
	m, srv := newStreamingModel(t, 150*time.Millisecond, func(w http.ResponseWriter, r *http.Request) {
		// Never answers in time; the client gives up first.
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})

	m, cmd := sendTypedURL(t, m, srv.URL+"/slow")
	m = drainStream(t, m, cmd)

	assert.False(t, m.Loading())
	assertViewContains(t, m, "Request timed out")
}

func TestE2E_RequestTimeoutWhileReadingBodyIsReported(t *testing.T) {
	m, srv := newStreamingModel(t, 200*time.Millisecond, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"partial":`)
		_ = http.NewResponseController(w).Flush()
		// The body never completes, so the timeout fires mid-read.
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})

	m, cmd := sendTypedURL(t, m, srv.URL+"/stalled-body")
	m = drainStream(t, m, cmd)

	assert.False(t, m.Loading())
	assertViewContains(t, m, "Request timed out")
}
