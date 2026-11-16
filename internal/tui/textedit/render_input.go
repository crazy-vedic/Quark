package textedit

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// cursorStyle draws the cursor cell. It matches the textinput default, which
// reverses the character under the cursor.
var cursorStyle = lipgloss.NewStyle().Reverse(true)

// RenderInput draws a single-line field with its selection. It keeps the
// widget's prompt, placeholder and width, and it scrolls so the cursor stays
// visible. The selection is drawn in st and the cursor cell is reversed.
//
// The widget's own scroll offset is not exported, so the window is computed
// here from the cursor on every render. The window is measured in display
// cells, so a wide rune such as CJK takes two.
func RenderInput(input *textinput.Model, sel Selection, st lipgloss.Style) string {
	runes := []rune(input.Value())
	if len(runes) == 0 && input.Placeholder != "" {
		return input.View()
	}
	pos := clamp(input.Position(), len(runes))
	start, end := sel.Range()
	start, end = clamp(start, len(runes)), clamp(end, len(runes))

	// Width 0 means no window: the whole value is shown, and the caller's pane
	// clips it. Real fields always set Width, so the cursor stays visible.
	offset, visibleEnd := 0, len(runes)
	if w := input.Width; w > 0 {
		offset, visibleEnd = cellWindow(runes, pos, w)
	}

	var b strings.Builder
	b.WriteString(input.PromptStyle.Render(input.Prompt))
	for i := offset; i < visibleEnd; i++ {
		r := string(runes[i])
		switch {
		case i == pos:
			b.WriteString(cursorStyle.Render(r))
		case i >= start && i < end:
			b.WriteString(st.Render(r))
		default:
			b.WriteString(input.TextStyle.Render(r))
		}
	}
	if pos == len(runes) {
		// The cursor sits after the last rune and needs its own cell.
		b.WriteString(cursorStyle.Render(" "))
	}
	return b.String()
}

// cellWindow returns the rune range [offset, end) that fits in w display cells
// and keeps the cursor cell visible. A cursor after the last rune needs one
// blank cell of its own.
func cellWindow(runes []rune, pos, w int) (offset, end int) {
	cursorCells := 1
	if pos < len(runes) {
		cursorCells = cellWidth(runes[pos])
	}

	// Scroll left from the cursor while the cursor cell still fits in w.
	offset, used := pos, cursorCells
	for offset > 0 {
		rw := cellWidth(runes[offset-1])
		if used+rw > w {
			break
		}
		used += rw
		offset--
	}

	// Fill right from offset, leaving the trailing cursor cell free if needed.
	budget := w
	if pos == len(runes) {
		budget--
	}
	end, cells := offset, 0
	for end < len(runes) {
		rw := cellWidth(runes[end])
		if cells+rw > budget {
			break
		}
		cells += rw
		end++
	}
	return offset, end
}

// cellWidth is the number of display cells a rune occupies.
func cellWidth(r rune) int {
	return lipgloss.Width(string(r))
}
