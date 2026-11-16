package textedit

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

type nopClipboard struct{}

func (nopClipboard) Read() (string, error)   { return "", nil }
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
