//go:build e2e

package tui_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestE2E_StartupRestoresRequestAndHistory(t *testing.T) {
	col := &domain.Collection{ID: uuid.NewString(), Name: "Startup"}
	st := setupStore(t, col)
	first := &domain.Request{ID: uuid.NewString(), Name: "First", Method: "GET", URL: "https://first.example"}
	last := &domain.Request{ID: uuid.NewString(), Name: "Last", Method: "GET", URL: "https://last.example"}
	seedRequests(t, st, col.ID, first, last)
	require.NoError(t, st.SaveExecution(context.Background(), &domain.Execution{
		ID:              uuid.NewString(),
		RequestID:       last.ID,
		RequestSnapshot: `{"method":"GET","url":"https://last.example"}`,
		StatusCode:      200,
		ResponseHeaders: `{}`,
		ResponseBody:    `{"restored":true}`,
		StartedAt:       time.Now().Add(-time.Minute),
		CompletedAt:     time.Now().Add(-time.Minute),
	}))

	cfg := config.Default(t.TempDir())
	cfg.UI.LastRequestID = last.ID
	m := newE2EModelWithConfig(t, st, &mockExecutor{}, cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{first, last}))
	m = executeCmdUpdate(t, m, cmd)

	require.Equal(t, last.ID, m.ActiveRequest().ID)
	require.Len(t, m.Executions(), 1)
	assert.Equal(t, 200, m.Executions()[0].StatusCode)
}

func TestE2E_TUIFileBodyHeaderAndJSONReachHTTPServer(t *testing.T) {
	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	headerPath := filepath.Join(t.TempDir(), "token.txt")
	require.NoError(t, os.WriteFile(payloadPath, []byte(`{"name":"quark","items":[1,2]}`), 0o600))
	require.NoError(t, os.WriteFile(headerPath, []byte("from-file"), 0o600))

	var receivedBody, receivedToken, receivedHost string
	var receivedTransfer []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		receivedToken = r.Header.Get("X-Token")
		receivedHost = r.Host
		receivedTransfer = append([]string(nil), r.TransferEncoding...)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local socket unavailable in this environment: %v", err)
	}
	srv := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	srv.Start()
	t.Cleanup(srv.Close)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)

	col := &domain.Collection{ID: uuid.NewString(), Name: "Files"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:      uuid.NewString(),
		Name:    "File request",
		Method:  "POST",
		URL:     srv.URL,
		Body:    "@" + payloadPath,
		Headers: `{"Content-Type":"application/json; charset=utf-8","X-Token":"@` + headerPath + `","Host":"e2e.example"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, exec.New(transport))
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = executeCmdUpdate(t, m, cmd)

	assert.False(t, m.Loading())
	assert.Equal(t, 200, m.Response().StatusCode)
	assert.Equal(t, "e2e.example", receivedHost)
	assert.Equal(t, "from-file", receivedToken)
	assert.Equal(t, "{\n  \"name\": \"quark\",\n  \"items\": [\n    1,\n    2\n  ]\n}", receivedBody)
	assert.Empty(t, receivedTransfer)
}
