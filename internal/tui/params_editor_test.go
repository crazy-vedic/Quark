package tui_test

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

func paramsEditorModel(t *testing.T, req *domain.Request) (tui.Model, *store.Store) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "test.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))
	req.ID = uuid.New().String()
	req.CollectionID = col.ID
	req.Name = "List"
	req.Method = "GET"
	require.NoError(t, st.SaveRequest(ctx, req))

	m := tui.New(
		tui.Deps{Lister: st, Reader: st, Writer: st, Config: config.Default(t.TempDir()), Ctx: ctx},
	)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = m.WithActiveRequest(req).WithURLValue(req.URL).WithFocus(tui.RequestPane)
	return m, st
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func typeText(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	for _, r := range s {
		m = callUpdate(t, m, runeKey(r))
	}
	return m
}

func TestParamsEditor_EditsQueryAndKeepsURLInSync(t *testing.T) {
	req := &domain.Request{
		URL:    "https://api.test/items?a=1&b=2#top",
		Params: `{"disabled_query":[{"key":"debug","value":"1"}]}`,
	}
	m, st := paramsEditorModel(t, req)

	m = callUpdate(t, m, runeKey('p'))
	require.Equal(t, tui.ParamsField, m.ActiveField())
	pairs := m.HeaderPairs()
	require.Len(t, pairs, 3)
	assert.Equal(t, "debug", pairs[2].Key)
	assert.True(t, pairs[2].Disabled)
	view := m.View()
	assert.Contains(t, view, "[x] a=1")
	assert.Contains(t, view, "[ ] debug=1")

	// Disable b, enable debug: the URL follows at once.
	m = callUpdate(t, m, runeKey('j'))
	m = callUpdate(t, m, runeKey('t'))
	assert.Equal(t, "https://api.test/items?a=1#top", m.URLValue())
	m = callUpdate(t, m, runeKey('j'))
	m = callUpdate(t, m, runeKey('t'))
	assert.Equal(t, "https://api.test/items?a=1&debug=1#top", m.URLValue())

	// Add a row; Tab switches to the value without header suggestions.
	m = callUpdate(t, m, runeKey('a'))
	require.True(t, m.HeaderEditing())
	m = typeText(t, m, "Acc")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "Acc", m.HeaderKeyValue())
	m = typeText(t, m, "x y")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "https://api.test/items?a=1&debug=1&Acc=x%20y#top", m.URLValue())

	// Delete the first row.
	for range 3 {
		m = callUpdate(t, m, runeKey('k'))
	}
	m = callUpdate(t, m, runeKey('d'))
	assert.Equal(t, "https://api.test/items?debug=1&Acc=x%20y#top", m.URLValue())

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "Query params updated", m.StatusSuccess())
	cmd()

	saved, err := st.GetRequest(context.Background(), req.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://api.test/items?debug=1&Acc=x%20y#top", saved.URL)
	stored, err := domain.ParseRequestParams(saved.Params)
	require.NoError(t, err)
	require.Len(t, stored.DisabledQuery, 1)
	assert.Equal(t, "b", stored.DisabledQuery[0].Key)

	// Reopening lists the disabled param after the URL params.
	m = callUpdate(t, m, runeKey('p'))
	pairs = m.HeaderPairs()
	require.Len(t, pairs, 3)
	assert.Equal(t, "b", pairs[2].Key)
	assert.True(t, pairs[2].Disabled)
}

func TestParamsEditor_EscRestoresURL(t *testing.T) {
	m, _ := paramsEditorModel(t, &domain.Request{URL: "https://api.test/?a=1"})
	m = callUpdate(t, m, runeKey('p'))
	m = callUpdate(t, m, runeKey('t'))
	assert.Equal(t, "https://api.test/", m.URLValue())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "https://api.test/?a=1", m.URLValue())
	assert.Equal(t, "https://api.test/?a=1", m.ActiveRequest().URL)
}

func TestParamsEditor_EmptyAndInvalidStates(t *testing.T) {
	m, _ := paramsEditorModel(t, &domain.Request{URL: "https://api.test/"})
	m = callUpdate(t, m, runeKey('p'))
	assert.Contains(t, m.View(), "No query params")
	// Toggle and delete on an empty list are no-ops.
	m = callUpdate(t, m, runeKey('t'))
	m = callUpdate(t, m, runeKey('d'))
	assert.Equal(t, "https://api.test/", m.URLValue())

	bad, _ := paramsEditorModel(t, &domain.Request{URL: "https://api.test/", Params: "{bad"})
	bad = callUpdate(t, bad, runeKey('p'))
	assert.Equal(t, tui.NoneField, bad.ActiveField())
	assert.Contains(t, bad.StatusErr(), "not valid JSON")

	none := newModel(defaultConfig()).WithFocus(tui.RequestPane)
	none = callUpdate(t, none, runeKey('p'))
	assert.Equal(t, "Select a request first", none.StatusErr())
}

func TestParamsEditor_EditExistingRowKeepsDisabledState(t *testing.T) {
	m, _ := paramsEditorModel(t, &domain.Request{
		URL:    "https://api.test/",
		Params: `{"disabled_query":[{"key":"x","value":"1"}]}`,
	})
	m = callUpdate(t, m, runeKey('p'))
	m = callUpdate(t, m, runeKey('e'))
	require.True(t, m.HeaderEditing())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "y")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	pairs := m.HeaderPairs()
	require.Len(t, pairs, 1)
	assert.Equal(t, "xy", pairs[0].Key)
	assert.True(t, pairs[0].Disabled)
	assert.Equal(t, "https://api.test/", m.URLValue())
}
