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

func choosePaletteCommand(t *testing.T, m tui.Model, query string) (tui.Model, tea.Cmd) {
	t.Helper()
	m = resize(t, m, 120, 60)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range ">" + query {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	require.Len(t, m.CommandResults(), 1)
	return callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

func TestE2E_CommandPalette_QuitAndSearch(t *testing.T) {
	newModel := func() tui.Model {
		return newE2EModel(t, setupStore(t), &mockExecutor{})
	}

	quitModel, quitCmd := choosePaletteCommand(t, newModel(), "quit quark")
	require.NotNil(t, quitCmd)
	assert.IsType(t, tea.QuitMsg{}, runCmd(t, quitCmd))
	assert.Equal(t, tui.NormalMode, quitModel.Mode())

	searchModel, searchCmd := choosePaletteCommand(t, newModel(), "search saved requests")
	require.NotNil(t, searchCmd)
	assert.Equal(t, tui.SearchMode, searchModel.Mode())
	assert.Empty(t, searchModel.SearchInputValue())
}

func TestE2E_CommandPalette_RequestAndCollectionActions(t *testing.T) {
	col := &domain.Collection{ID: "col-palette", Name: "Palette"}
	req := &domain.Request{
		ID: "req-palette", CollectionID: col.ID, Name: "Health", Method: "GET",
		URL: "https://example.test/health",
	}

	newModel := func() tui.Model {
		st := setupStore(t, col)
		seedRequests(t, st, col.ID, req)
		m := newE2EModel(t, st, &mockExecutor{})
		m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
		return m.WithActiveRequest(req)
	}

	editAuth, _ := choosePaletteCommand(t, newModel(), "edit request authorization")
	assert.Equal(t, tui.AuthField, editAuth.ActiveField())

	deleteReq, _ := choosePaletteCommand(t, newModel(), "delete selected request")
	assert.Equal(t, tui.CollectionPromptMode, deleteReq.Mode())
	assert.Equal(t, tui.PromptDeleteTiny, deleteReq.PromptMode())

	addRequest, _ := choosePaletteCommand(t, newModel(), "create request in collection")
	assert.Equal(t, tui.PromptAddRequest, addRequest.PromptMode())

	renameCollection, _ := choosePaletteCommand(t, newModel(), "rename selected collection")
	assert.Equal(t, tui.PromptRename, renameCollection.PromptMode())

	deleteCollection, _ := choosePaletteCommand(t, newModel(), "delete selected collection")
	assert.Equal(t, tui.PromptDeleteConfirm, deleteCollection.PromptMode())
}

func TestE2E_CommandPalette_EnvironmentActions(t *testing.T) {
	col := &domain.Collection{ID: "col-env-palette", Name: "Environment Palette"}
	st := setupStore(t, col)
	base := newE2EModel(t, st, &mockExecutor{})
	base = callUpdate(t, base, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	openEditor, _ := choosePaletteCommand(t, base, "open environment editor")
	assert.Equal(t, tui.EnvMode, openEditor.Mode())
	assert.True(t, openEditor.EnvEditorActive())

	next, _ := choosePaletteCommand(t, base, "next environment")
	assert.NotEmpty(t, next.ActiveEnv()[col.ID])
	previous, _ := choosePaletteCommand(t, next, "previous environment")
	assert.Equal(t, next.ActiveEnv()[col.ID], previous.ActiveEnv()[col.ID])
}
