package tui_test

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestPathEditor_EditsValuesAndSaves(t *testing.T) {
	req := &domain.Request{
		URL:    "https://api.test/users/:id/items/:sub?x=1",
		Params: `{"path_values":{"id":"42","gone":"old"},"disabled_query":[{"key":"d","value":"1"}]}`,
	}
	m, st := paramsEditorModel(t, req)

	m = callUpdate(t, m, runeKey('P'))
	require.Equal(t, tui.PathField, m.ActiveField())
	pairs := m.HeaderPairs()
	require.Len(t, pairs, 2)
	assert.Equal(t, "id", pairs[0].Key)
	assert.Equal(t, "42", pairs[0].Value)
	assert.Equal(t, "sub", pairs[1].Key)
	assert.Empty(t, pairs[1].Value)
	view := m.View()
	assert.Contains(t, view, ":id = 42")
	assert.Contains(t, view, "(not set")

	// Move to :sub, set it to a variable, and confirm the row.
	m = callUpdate(t, m, runeKey('j'))
	m = callUpdate(t, m, runeKey('e'))
	require.True(t, m.HeaderEditing())
	m = typeText(t, m, "{{sub_value}}")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(
		t,
		"https://api.test/users/:id/items/:sub?x=1",
		m.URLValue(),
		"URL keeps placeholders",
	)

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "Path params updated", m.StatusSuccess())
	cmd()

	saved, err := st.GetRequest(context.Background(), req.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://api.test/users/:id/items/:sub?x=1", saved.URL)
	stored, err := domain.ParseRequestParams(saved.Params)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"id": "42", "sub": "{{sub_value}}"}, stored.PathValues,
		"values for placeholders that left the URL are dropped")
	require.Len(t, stored.DisabledQuery, 1, "query params editor state is kept")
	assert.Equal(t, "d", stored.DisabledQuery[0].Key)
}

func TestPathEditor_EscDiscardsValue(t *testing.T) {
	m, st := paramsEditorModel(t, &domain.Request{URL: "https://api.test/users/:id"})
	m = callUpdate(t, m, runeKey('P'))
	m = callUpdate(t, m, runeKey('e'))
	m = typeText(t, m, "99")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "https://api.test/users/:id", m.URLValue())

	saved, err := st.GetRequest(context.Background(), m.ActiveRequest().ID)
	require.NoError(t, err)
	stored, err := domain.ParseRequestParams(saved.Params)
	require.NoError(t, err)
	assert.Empty(t, stored.PathValues)
}

func TestPathEditor_NameIsFixed(t *testing.T) {
	m, _ := paramsEditorModel(t, &domain.Request{URL: "https://api.test/users/:id"})
	m = callUpdate(t, m, runeKey('P'))
	m = callUpdate(t, m, runeKey('e'))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "zz")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	pairs := m.HeaderPairs()
	require.Len(t, pairs, 1)
	assert.Equal(t, "id", pairs[0].Key)
	assert.Equal(t, "zz", pairs[0].Value)
}

func TestPathEditor_NoPlaceholdersIgnoresAddDeleteToggle(t *testing.T) {
	m, _ := paramsEditorModel(t, &domain.Request{URL: "https://api.test/items"})
	m = callUpdate(t, m, runeKey('P'))
	assert.Contains(t, m.View(), "No path placeholders")
	for _, r := range "adt" {
		m = callUpdate(t, m, runeKey(r))
	}
	assert.Empty(t, m.HeaderPairs())
	assert.Equal(t, "https://api.test/items", m.URLValue())
	assert.Equal(t, tui.PathField, m.ActiveField())
}

func TestPathEditor_InvalidParamsRefused(t *testing.T) {
	m, _ := paramsEditorModel(t, &domain.Request{URL: "https://api.test/:id", Params: "{bad"})
	m = callUpdate(t, m, runeKey('P'))
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Contains(t, m.StatusErr(), "not valid JSON")
}
