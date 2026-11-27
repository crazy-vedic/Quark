//go:build e2e

package tui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// Typing a custom method in the request pane saves it to the request and the
// next send uses it on the wire.
func TestE2E_TUITypedCustomMethodIsSavedAndSent(t *testing.T) {
	var mu sync.Mutex
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod = r.Method
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	col := &domain.Collection{ID: uuid.NewString(), Name: "Methods"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Listing",
		Method:       "GET",
		URL:          srv.URL + "/dav",
	}
	seedRequests(t, st, col.ID, req)

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	m := newE2EModel(t, st, exec.New(transport))
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	for range "GET" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	for _, r := range "propfind" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, saveCmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, m, saveCmd)
	assert.Equal(t, "PROPFIND", m.Method())

	stored, err := st.GetRequest(context.Background(), req.ID)
	require.NoError(t, err)
	assert.Equal(t, "PROPFIND", stored.Method)

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = executeCmdUpdate(t, m, cmd)
	require.NotNil(t, m.Response())
	assert.Equal(t, 200, m.Response().StatusCode)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "PROPFIND", gotMethod)
}
