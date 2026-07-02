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

// TestE2E_EnvEditor_OpenAndClose verifies pressing 'e' opens the env editor
// modal and Esc closes it.
func TestE2E_EnvEditor_OpenAndClose(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane first, then open env editor.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.Equal(t, tui.EnvMode, m.Mode(), "mode must be envMode")
	assert.True(t, m.EnvEditorActive(), "env editor must be active")
	assertViewContains(t, m, "Environment Variables")
	assertViewContains(t, m, "Global") // global tab is always present

	// Close with Esc.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "mode must return to normal")
	assert.False(t, m.EnvEditorActive(), "env editor must be inactive")
}

// TestE2E_EnvEditor_TabSwitching verifies left/right arrows switch tabs.
func TestE2E_EnvEditor_TabSwitching(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane and open env editor.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())

	// Tab 0 is Global, tab 1 is default (auto-created).
	assert.Equal(t, 0, m.EnvEditorTabIdx(), "must start on Global tab")

	// Right arrow moves to next tab.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, 1, m.EnvEditorTabIdx(), "must move to collection tab")

	// Left arrow moves back to global.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, 0, m.EnvEditorTabIdx(), "must move back to Global tab")
}

// TestE2E_EnvEditor_AddVariable verifies adding a key-value pair in the editor.
func TestE2E_EnvEditor_AddVariable(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane and open env editor.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())

	// Switch to collection tab (tab 1) to edit collection env.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, 1, m.EnvEditorTabIdx())

	// Press 'a' to add new variable.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.True(t, m.EnvEditorEditing(), "must enter editing sub-mode")

	// Type key "url" and value "http://localhost" via textinput routing.
	// The key input is focused.
	for _, r := range "url" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Tab to switch to value input.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "http://localhost" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Confirm with Enter.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.EnvEditorEditing(), "must exit editing sub-mode")

	// Verify the variable is in the list.
	vars := m.EnvEditorVars()
	require.Len(t, vars, 1)
	assert.Equal(t, "url", vars[0].Key)
	assert.Equal(t, "http://localhost", vars[0].Value)
}

// TestE2E_EnvEditor_SaveVariable verifies saving a variable persists to the env.
func TestE2E_EnvEditor_SaveVariable(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane and open env editor.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())

	// Switch to collection tab.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, 1, m.EnvEditorTabIdx())

	// Add a variable.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "url" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "http://localhost" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.EnvEditorEditing())

	// Save with 's' — the save command is dispatched async; verify no save error.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	assert.Empty(t, m.EnvEditorSaveErr(), "save must not error")

	// Vars remain in the editor until the modal is closed.
	vars := m.EnvEditorVars()
	require.Len(t, vars, 1)
	assert.Equal(t, "url", vars[0].Key)
	assert.Equal(t, "http://localhost", vars[0].Value)
}

// TestE2E_EnvEditor_DeleteVariable verifies deleting a variable.
func TestE2E_EnvEditor_DeleteVariable(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane and open env editor.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, 1, m.EnvEditorTabIdx())

	// Add a variable.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "url" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "http://localhost" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Len(t, m.EnvEditorVars(), 1)

	// Delete with 'd'.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.Len(t, m.EnvEditorVars(), 0, "variable must be deleted")
}

// TestE2E_EnvEditor_GlobalTabEdit verifies editing the global env tab.
func TestE2E_EnvEditor_GlobalTabEdit(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Open env editor (starts on Global tab).
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())
	assert.Equal(t, 0, m.EnvEditorTabIdx())

	// Add a global variable.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "api_key" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "secret123" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.EnvEditorEditing())

	vars := m.EnvEditorVars()
	require.Len(t, vars, 1)
	assert.Equal(t, "api_key", vars[0].Key)
	assert.Equal(t, "secret123", vars[0].Value)

	// Save with 's' — no error reported.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	assert.Empty(t, m.EnvEditorSaveErr(), "save must not error")
}

func TestE2E_EnvEditor_CreateEnv_UsesUppercaseA(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Open env editor and switch to the collection tab.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, 1, m.EnvEditorTabIdx())

	// Uppercase A should open the create-environment prompt.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode())
	assert.Equal(t, tui.PromptAddEnv, m.PromptMode())
	assertViewContains(t, m, "Environment name")
}

// TestE2E_EnvCycle_ShiftLeftRight verifies Shift+Left/Right cycles active env.
func TestE2E_EnvCycle_ShiftLeftRight(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Create a second env for the collection.
	ctx := m.ModelCtx()
	secondEnv := &domain.Environment{
		ID:           "env-2",
		CollectionID: col.ID,
		Name:         "dev",
		Data:         `{"url":"http://dev.local"}`,
	}
	require.NoError(t, st.SaveEnvironment(ctx, secondEnv))

	// Switch to request pane, then Shift+Right cycles to next env.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	activeEnv := m.ActiveEnv()
	require.NotNil(t, activeEnv)
	activeID := activeEnv[col.ID]
	assert.Equal(t, "env-2", activeID, "must cycle to second env")

	// Status bar shows active env name (rendered as ◀ dev ▶ in the title line).
	assertViewContains(t, m, "◀ dev ▶")

	// Switch to request pane, then Shift+Left cycles back.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	activeEnv = m.ActiveEnv()
	activeID = activeEnv[col.ID]
	// Default env is first.
	assert.NotEmpty(t, activeID)
}

// TestE2E_EnvCycle_NoEnvs shows graceful handling when no envs exist.
func TestE2E_EnvCycle_NoEnvs(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane, then Shift+Right with no envs should not crash.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Empty(t, m.StatusErr())
}

// TestE2E_EnvEditor_EditExistingVariable verifies editing an existing variable.
func TestE2E_EnvEditor_EditExistingVariable(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane and open env editor.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, 1, m.EnvEditorTabIdx())

	// Add a variable.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "url" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "old" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "old", m.EnvEditorVars()[0].Value)

	// Edit with 'e'.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.True(t, m.EnvEditorEditing(), "must enter editing sub-mode")

	// Tab to value field, clear existing value with Ctrl+U, type "new".
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	for _, r := range "new" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.EnvEditorEditing())
	assert.Equal(t, "new", m.EnvEditorVars()[0].Value)
}

// TestE2E_EnvEditor_UnsavedIndicator verifies that unsaved env vars show a *
// indicator before the key name, and that the indicator disappears after saving.
func TestE2E_EnvEditor_UnsavedIndicator(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Open env editor (Global tab).
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.Equal(t, tui.EnvMode, m.Mode())

	// Add a new global variable — it should be unsaved.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "token" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "abc123" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.EnvEditorEditing())

	vars := m.EnvEditorVars()
	require.Len(t, vars, 1)
	assert.False(t, vars[0].Saved, "newly added variable must be unsaved")

	// Rendered view must contain the * indicator before the key name.
	assertViewContains(t, m, "token*")

	// Save the environment — * should disappear.
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	msg := runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)

	vars = m.EnvEditorVars()
	require.Len(t, vars, 1)
	assert.True(t, vars[0].Saved, "variable must be marked saved after save")
	assertViewNotContains(t, m, "token*")
}
