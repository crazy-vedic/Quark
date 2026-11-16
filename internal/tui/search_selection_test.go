package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/tui"
)

func newSearchWithText(text string) tui.Model {
	return newModel(defaultConfig()).
		WithMode(tui.SearchMode).
		WithSearchInputValue(text).
		WithSearchEdit()
}

func TestSearchCtrlCCopiesSelectionOnly(t *testing.T) {
	m := newSearchWithText("hello")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, "hello", m.SearchInputValue(), "Ctrl+C with no selection keeps text")

	m = newSearchWithText("hello").WithSearchSelection(0, 5)
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd, "Ctrl+C with a selection copies")
	assert.Equal(t, "hello", m2.SearchInputValue(), "copy leaves the text")
	assert.Equal(t, tui.SearchMode, m2.Mode(), "Ctrl+C does not close search")
}

func TestSearchCtrlXCutsSelection(t *testing.T) {
	m := newSearchWithText("hello world").WithSearchSelection(6, 11)
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd, "cut writes to the clipboard")
	assert.Equal(t, "hello ", m2.SearchInputValue())
	assert.False(t, m2.SearchSelection().Active())
}

func TestSearchTypingReplacesSelection(t *testing.T) {
	m := newSearchWithText("hello world").WithSearchSelection(0, 5)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.Equal(t, "x world", m.SearchInputValue())
	assert.False(t, m.SearchSelection().Active())
}

func TestSearchSelectionSurvivesUnownedKey(t *testing.T) {
	m := newSearchWithText("hello").WithSearchSelection(1, 3)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyF12})
	assert.True(t, m.SearchSelection().Active(), "an unowned key keeps the selection")
}

func TestSearchSelectAllThenPasteReplaces(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "new", nil })
	t.Cleanup(restore)

	m := newSearchWithText("old")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.True(t, m.SearchSelection().Active())
	m = callPaste(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "new", m.SearchInputValue())
}

func TestSearchDownWithSelectionGoesToResolver(t *testing.T) {
	m := newSearchWithText("hello").WithSearchSelection(1, 3)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.True(t, m.SearchSelection().Active(), "Down is routed to the resolver, not the editor, so the selection is kept")
}

func TestSearchArrowWithoutTextChangeDoesNotRerunSearch(t *testing.T) {
	m := newSearchWithText("hello")
	require.Nil(t, m.SearchCancel())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assert.Nil(t, m.SearchCancel(), "moving the cursor does not change the query, so no search is dispatched")
	assert.Equal(t, "hello", m.SearchInputValue())
}
