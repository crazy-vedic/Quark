package textedit

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

type nopClipboard struct{}

func (nopClipboard) ReadCmd() tea.Cmd        { return nil }
func (nopClipboard) WriteCmd(string) tea.Cmd { return nil }

func TestInputAdapterRoundTrip(t *testing.T) {
	m := textinput.New()
	m.SetValue("https://x.dev/a")
	b := Input(&m)
	assert.Equal(t, "https://x.dev/a", b.Text())
	b.SetCursor(5)
	assert.Equal(t, 5, b.Cursor())
	assert.False(t, b.MultiLine())
}

func TestInputSelectionAndTypingThroughRealWidget(t *testing.T) {
	m := textinput.New()
	m.Focus()
	m.SetValue("hello")
	m.SetCursor(0)
	ed := New(nopClipboard{})
	var sel Selection
	sel.Collapse(0)
	b := Input(&m)

	ed.HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyCtrlA})
	_, handled := ed.HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	assert.True(t, handled)
	assert.Equal(t, "J", m.Value())
	assert.Equal(t, 1, m.Position())
}

func TestInputShiftLeftThenBackspaceOnRealWidget(t *testing.T) {
	m := textinput.New()
	m.Focus()
	m.SetValue("abcd")
	m.SetCursor(4)
	ed := New(nopClipboard{})
	var sel Selection
	sel.Collapse(4)
	b := Input(&m)

	ed.HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyShiftLeft})
	ed.HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyShiftLeft})
	ed.HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "ab", m.Value())
	assert.Equal(t, 2, m.Position())
}

// Emacs-style delete keys must respect an active selection, like Backspace
// and Delete do. The widget otherwise deletes by its own rule and Sync keeps
// a selection whose range no longer fits the text.
func TestEmacsDeleteKeysDeleteSelectionOnRealWidget(t *testing.T) {
	cases := []struct {
		name     string
		msg      tea.KeyMsg
		anchor   int
		cursor   int
		wantText string
	}{
		{"ctrl+k", tea.KeyMsg{Type: tea.KeyCtrlK}, 11, 6, "hello "},
		{"ctrl+u", tea.KeyMsg{Type: tea.KeyCtrlU}, 0, 5, " world"},
		{"ctrl+d", tea.KeyMsg{Type: tea.KeyCtrlD}, 0, 5, " world"},
		{"alt+d", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true}, 0, 5, " world"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := textinput.New()
			m.Focus()
			m.SetValue("hello world")
			ed := New(nopClipboard{})
			var sel Selection
			sel.SelectRange(tc.anchor, tc.cursor)
			b := Input(&m)
			b.SetCursor(tc.cursor)

			_, handled := ed.HandleKey(&sel, b, tc.msg)
			assert.True(t, handled)
			assert.Equal(t, tc.wantText, m.Value())
			assert.False(t, sel.Active(), "selection must be removed with the text")
			assert.LessOrEqual(t, sel.Cursor, len([]rune(m.Value())))
			assert.LessOrEqual(t, sel.Anchor, len([]rune(m.Value())))
		})
	}
}
