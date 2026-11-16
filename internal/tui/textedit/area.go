package textedit

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type AreaBackend struct{ m *textarea.Model }

// Area adapts a bubbles textarea. Offsets are absolute rune offsets into the
// full text, not row offsets: the textarea reports positions per wrapped row,
// so Cursor and SetCursor translate between the two.
func Area(m *textarea.Model) AreaBackend { return AreaBackend{m} }

func (b AreaBackend) Text() string     { return b.m.Value() }
func (b AreaBackend) SetText(s string) { b.m.SetValue(s) }
func (b AreaBackend) MultiLine() bool  { return true }

func (b AreaBackend) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	*b.m, cmd = b.m.Update(msg)
	return cmd
}

// Cursor returns the absolute rune offset of the cursor. LineInfo.StartColumn
// is the first column of the wrapped row within its logical line, and
// ColumnOffset is the cursor's column within that row.
func (b AreaBackend) Cursor() int {
	li := b.m.LineInfo()
	runes := []rune(b.m.Value())
	return lineStartOffset(runes, b.m.Line()) + li.StartColumn + li.ColumnOffset
}

// SetCursor moves the cursor to an absolute rune offset. The textarea has no
// absolute setter and CursorUp/CursorDown move by wrapped row, so the cursor
// goes to the top, down to the target logical line, then to the column.
func (b AreaBackend) SetCursor(pos int) {
	text := b.m.Value()
	runes := []rune(text)
	pos = clamp(pos, len(runes))
	line, col := lineColAt(runes, pos)
	limit := len(runes) + strings.Count(text, "\n") + 8
	for i := 0; i < limit && b.m.Line() > 0; i++ {
		b.m.CursorUp()
	}
	for i := 0; i < limit && b.m.Line() < line; i++ {
		b.m.CursorDown()
	}
	b.m.SetCursor(col)
}

// MoveVertical moves one wrapped row and returns the new absolute cursor.
func (b AreaBackend) MoveVertical(delta int) int {
	if delta < 0 {
		b.m.CursorUp()
	} else {
		b.m.CursorDown()
	}
	return b.Cursor()
}

// lineStartOffset returns the rune offset where logical line `line` starts.
func lineStartOffset(runes []rune, line int) int {
	idx := 0
	for l := 0; l < line && idx < len(runes); l++ {
		for idx < len(runes) && runes[idx] != '\n' {
			idx++
		}
		idx++
	}
	return min(idx, len(runes))
}

// lineColAt converts an absolute offset to a logical line and column.
func lineColAt(runes []rune, pos int) (line, col int) {
	start := 0
	for i := 0; i < pos && i < len(runes); i++ {
		if runes[i] == '\n' {
			line++
			start = i + 1
		}
	}
	return line, pos - start
}
