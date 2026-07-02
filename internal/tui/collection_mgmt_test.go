package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// --- Add collection ---

func TestCollectionPrompt_Add_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'A' must enter collection prompt mode",
	)
	assert.Equal(t, tui.PromptAdd, m.PromptMode(), "prompt mode must be PromptAdd")
}

func TestCollectionPrompt_Add_CancelOnEscape(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel back to normal mode")
}

func TestCollectionPrompt_Add_EmptyNameShowsError(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"empty name must stay in prompt so user can retry",
	)
	assert.NotEmpty(t, m.StatusErr(), "must show error for empty name")
}

func TestCollectionPrompt_AddRequest_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'a' with a collection selected must enter request prompt mode",
	)
	assert.Equal(t, tui.PromptAddRequest, m.PromptMode(), "prompt mode must be PromptAddRequest")
}

// --- Rename collection ---

func TestCollectionPrompt_Rename_RequiresSelection(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	assert.Equal(
		t,
		tui.NormalMode,
		m.Mode(),
		"pressing 'r' with no collection selected must not enter prompt mode",
	)
	assert.NotEmpty(t, m.StatusErr(), "must show error when no collection selected")
}

func TestCollectionPrompt_Rename_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'r' with a collection selected must enter prompt mode",
	)
	assert.Equal(t, tui.PromptRename, m.PromptMode(), "prompt mode must be PromptRename")
}

func TestCollectionPrompt_Rename_CancelOnEscape(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel rename prompt")
}

// --- Delete collection ---

func TestCollectionPrompt_Delete_RequiresSelection(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	assert.Equal(
		t,
		tui.NormalMode,
		m.Mode(),
		"pressing 'd' with no collection selected must not enter prompt mode",
	)
	assert.NotEmpty(t, m.StatusErr(), "must show error when no collection selected")
}

func TestCollectionPrompt_Delete_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'd' with a collection selected must enter prompt mode",
	)
	assert.Equal(t, tui.PromptDeleteTiny, m.PromptMode(), "prompt mode must be PromptDeleteTiny")
}

func TestCollectionPrompt_Delete_CancelOnEscape(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel delete prompt")
}

func TestCollectionPrompt_Delete_UsesConfiguredConfirmKey(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.SidebarDelete = "x"
	m := newModel(cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	assert.Contains(t, m.View(), "[x] confirm")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "configured delete key must confirm the tiny prompt")
	assert.NotEmpty(
		t,
		m.StatusErr(),
		"without a writer the prompt should still attempt deletion and surface an error",
	)
}

func TestCollectionPrompt_UsesConfiguredConfirmAndCancelBindings(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.ImportConfirm = "x"
	cfg.Keybindings.ImportCancel = "c"
	m := newModel(cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.WithFocus(tui.SidebarPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())
	assert.Contains(t, m.View(), "[x] confirm")
	assert.Contains(t, m.View(), "[c] cancel")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "configured cancel key must close the prompt")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"configured confirm key must trigger prompt submission",
	)
	assert.NotEmpty(
		t,
		m.StatusErr(),
		"confirming an empty prompt should surface the validation error",
	)
}

func TestCollectionPrompt_Delete_TypingNoShowsError(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode(), "pressing 'd' must enter prompt mode")

	// In the tiny prompt, pressing any key other than 'd' just keeps the prompt open
	// (the text input still receives the characters, but Enter is a no-op).
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing Enter with non-'d' must stay in prompt mode",
	)
	assert.Empty(t, m.StatusErr(), "no error shown for tiny prompt")

	// Esc cancels the prompt and returns to normal mode.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel delete prompt")
}
