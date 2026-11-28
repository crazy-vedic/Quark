//go:build e2e

package tui_test

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

const queryParamsTestURL = "https://example.com/items?page=1&q=hello%20world"

// openQueryParamsEditor seeds one request with the given stored params JSON
// and opens the query param editor on it with 'p'.
func openQueryParamsEditor(t *testing.T, params string) (tui.Model, *domain.Request, *store.Store) {
	t.Helper()
	col := &domain.Collection{ID: uuid.NewString(), Name: "Params"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:     uuid.NewString(),
		Name:   "Search",
		Method: "GET",
		URL:    queryParamsTestURL,
		Params: params,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	return m, req, st
}

func typeRunes(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	return callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func TestE2E_QueryParams_OpensFromURLWithDecodedValues(t *testing.T) {
	m, _, _ := openQueryParamsEditor(t, "")

	assert.Equal(t, tui.ParamsField, m.ActiveField())
	require.Len(t, m.HeaderPairs(), 2)
	assert.Equal(t, "page", m.HeaderPairs()[0].Key)
	assert.Equal(t, "1", m.HeaderPairs()[0].Value)
	assert.Equal(t, "q", m.HeaderPairs()[1].Key)
	assert.Equal(t, "hello world", m.HeaderPairs()[1].Value)
	assertViewContains(t, m, "[x] page=1")
	assertViewContains(t, m, "[x] q=hello world")
}

func TestE2E_QueryParams_EmptyURLShowsAddHint(t *testing.T) {
	col := &domain.Collection{ID: uuid.NewString(), Name: "Params"}
	st := setupStore(t, col)
	req := &domain.Request{ID: uuid.NewString(), Name: "Bare", Method: "GET", URL: "https://example.com/"}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})

	assert.Equal(t, tui.ParamsField, m.ActiveField())
	assert.Empty(t, m.HeaderPairs())
	assertViewContains(t, m, "No query params.")
}

func TestE2E_QueryParams_InvalidStoredParamsRefuseEdit(t *testing.T) {
	m, _, _ := openQueryParamsEditor(t, "not-json")
	// The editor refuses to open over stored params it cannot decode.
	assert.NotEqual(t, tui.ParamsField, m.ActiveField())
	assertViewContains(t, m, "not valid JSON")
}

func TestE2E_QueryParams_ToggleDisablesParamInURLAndSaves(t *testing.T) {
	m, req, st := openQueryParamsEditor(t, "")

	// The cursor starts on the first row; 't' switches it off.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	require.True(t, m.HeaderPairs()[0].Disabled)
	assert.NotContains(t, m.URLValue(), "page=1")
	assert.Contains(t, m.URLValue(), "q=hello%20world")
	assertViewContains(t, m, "[ ] page=1")

	// Enter saves; the disabled row survives in the stored params, not the URL.
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, tui.NoneField, m.ActiveField())
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, req.ID, m.ActiveRequest().ID)
	assert.NotContains(t, m.ActiveRequest().URL, "page=1")
	assert.Contains(t, m.ActiveRequest().Params, `"page"`)

	saved, err := st.GetRequest(context.Background(), req.ID)
	require.NoError(t, err)
	assert.NotContains(t, saved.URL, "page=1")
	assert.Contains(t, saved.Params, `"disabled_query"`)
	assert.Contains(t, saved.Params, `"page"`)
}

func TestE2E_QueryParams_ReopenKeepsDisabledRows(t *testing.T) {
	m, _, st := openQueryParamsEditor(t, `{"disabled_query":[{"key":"debug","value":"1"}]}`)

	require.Len(t, m.HeaderPairs(), 3, "two URL params plus one stored disabled row")
	last := m.HeaderPairs()[2]
	assert.Equal(t, "debug", last.Key)
	assert.True(t, last.Disabled)
	assertViewContains(t, m, "[ ] debug=1")

	// Saving without changes must keep the disabled row in the stored params.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.Len(t, m.HeaderPairs(), 3)
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = executeCmdUpdate(t, m, cmd)
	saved, err := st.GetRequest(context.Background(), m.ActiveRequest().ID)
	require.NoError(t, err)
	assert.Contains(t, saved.Params, `"debug"`)
}

func TestE2E_QueryParams_AddRowWritesURL(t *testing.T) {
	m, _, _ := openQueryParamsEditor(t, "")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.True(t, m.HeaderEditing(), "add must open the row editor")
	m = typeRunes(t, m, "limit")
	assert.Equal(t, "limit", m.HeaderKeyValue())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeRunes(t, m, "10")
	assert.Equal(t, "10", m.HeaderValueValue())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.False(t, m.HeaderEditing())
	require.Len(t, m.HeaderPairs(), 3)
	assert.Contains(t, m.URLValue(), "limit=10")
	assertViewContains(t, m, "[x] limit=10")
}

func TestE2E_QueryParams_EditRowRewritesURL(t *testing.T) {
	m, _, _ := openQueryParamsEditor(t, "")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.True(t, m.HeaderEditing())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeRunes(t, m, "2")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.False(t, m.HeaderEditing())

	assert.Equal(t, "2", m.HeaderPairs()[0].Value)
	assert.Contains(t, m.URLValue(), "page=2")
	assert.NotContains(t, m.URLValue(), "page=1")
}

func TestE2E_QueryParams_DeleteRowRemovesFromURL(t *testing.T) {
	m, _, _ := openQueryParamsEditor(t, "")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Len(t, m.HeaderPairs(), 1)
	assert.NotContains(t, m.URLValue(), "page=1")
	assert.Contains(t, m.URLValue(), "q=hello%20world")
}

func TestE2E_QueryParams_EscRestoresURL(t *testing.T) {
	m, _, _ := openQueryParamsEditor(t, "")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.NotContains(t, m.URLValue(), "page=1")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, queryParamsTestURL, m.URLValue(), "cancel must put the original URL back")
}
