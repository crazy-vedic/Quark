package textedit

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type InputBackend struct{ m *textinput.Model }

// Input adapts a bubbles textinput. The adapter holds only a pointer to the
// widget and keeps no state of its own.
func Input(m *textinput.Model) InputBackend { return InputBackend{m} }

func (b InputBackend) Text() string     { return b.m.Value() }
func (b InputBackend) SetText(s string) { b.m.SetValue(s) }
func (b InputBackend) Cursor() int      { return b.m.Position() }
func (b InputBackend) SetCursor(p int)  { b.m.SetCursor(p) }
func (b InputBackend) MultiLine() bool  { return false }

func (b InputBackend) MoveVertical(int) int { return b.m.Position() }

func (b InputBackend) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	*b.m, cmd = b.m.Update(msg)
	return cmd
}
