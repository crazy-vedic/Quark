//go:build e2e

package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// typeBodyAndSave opens the body editor on a JSON request, types text and saves.
func typeBodyAndSave(t *testing.T, text string) *domain.Request {
	t.Helper()
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.com/items",
		Headers: `{"Content-Type":"application/json"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	return m.ActiveRequest()
}

// A body with an unquoted placeholder saves exactly as typed.
func TestE2E_BodyEditor_SavesJSONTemplate(t *testing.T) {
	saved := typeBodyAndSave(t, `{"id":{{id}}}`)
	require.NotNil(t, saved)
	assert.Equal(t, `{"id":{{id}}}`, saved.Body)
}
