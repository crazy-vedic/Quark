package tui_test

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

func pasteFixture(value string) tui.Model {
	return newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithActiveRequest(existingRequest()).
		WithURLEdit().
		WithURLValue(value)
}

// Ctrl+V reads the clipboard asynchronously. The command it returns is run
// later, after focus may have changed. These tests hold that command and
// deliver it at a different time, as the Bubble Tea loop would.

func TestPasteDroppedWhenFocusLeavesFieldBeforeRead(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "pasted", nil })
	t.Cleanup(restore)

	m, cmd := callUpdateWithCmd(t, pasteFixture("keep"), tea.KeyMsg{Type: tea.KeyCtrlV})
	require.NotNil(t, cmd)
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	before := m.URLValue()

	m = feedPastes(t, m, cmd)
	assert.Equal(t, before, m.URLValue(), "text must not land after focus left the field")
	assert.NotContains(t, m.URLValue(), "pasted")
}

func TestPasteInsertedWhenFocusUnchanged(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "pasted", nil })
	t.Cleanup(restore)

	m := callPaste(t, pasteFixture(""), tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "pasted", m.URLValue())
}

func TestOlderPasteRequestIsDropped(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "X", nil })
	t.Cleanup(restore)

	m := pasteFixture("")
	m, first := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	m, second := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})

	m = feedPastes(t, m, first)
	assert.Equal(t, "", m.URLValue(), "a superseded paste must not insert")
	m = feedPastes(t, m, second)
	assert.Equal(t, "X", m.URLValue())
}

// A paste result delivered in normal mode must not reach the global shortcut
// path. "q" would quit the app there, so the test checks that no quit follows.
func TestPasteInNormalModeNeverFiresShortcut(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "q", nil })
	t.Cleanup(restore)

	m, cmd := callUpdateWithCmd(t, pasteFixture(""), tea.KeyMsg{Type: tea.KeyCtrlV})
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	res, ok := cmd().(textedit.PasteResultMsg)
	require.True(t, ok)
	_, next := callUpdateWithCmd(t, m, res)
	if next != nil {
		_, isQuit := next().(tea.QuitMsg)
		assert.False(t, isQuit, "pasted q must not quit the app")
	}
}

// A terminal bracketed paste with no focused field is dropped, not treated as keys.
func TestBracketedPasteInNormalModeIsIgnored(t *testing.T) {
	m := newModelWithCurl().WithFocus(tui.RequestPane)
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Paste: true}
	m2, cmd := callUpdateWithCmd(t, m, key)
	assert.Nil(t, cmd, "pasted q must not quit")
	assert.Contains(t, m2.StatusErr(), "Paste ignored")
}

func TestPasteReadErrorIsReported(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) {
		return "", errors.New("no xclip")
	})
	t.Cleanup(restore)

	m := callPaste(t, pasteFixture("keep"), tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "keep", m.URLValue())
	assert.True(t, strings.Contains(m.StatusErr(), "Clipboard read failed"), m.StatusErr())
}

// Ctrl+V in normal mode (no focused field) is a no-op. Copy in normal mode is
// also a no-op, so paste does not start editing the URL. See review S4.
func TestCtrlVInNormalModeIsNoOp(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "pasted", nil })
	t.Cleanup(restore)

	m := newModelWithCurl().WithFocus(tui.RequestPane).WithActiveRequest(existingRequest())
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Nil(t, cmd)
	assert.Equal(t, "", m2.URLValue())
}
