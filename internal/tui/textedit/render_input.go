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
// here from the cursor on every render. Each rune counts as one cell.
//
// The cursor cell is drawn only while input is focused, so an unfocused field
// (such as the Key or Value of a non-active header or env row) shows no cursor.
//
// Blink is dropped on purpose. The widget's blink state only changes when its
// cursor tick messages are delivered, and this function is a pure render that
// does not receive them. Reading that state here would freeze the cursor in
// whatever phase it was last in, so the focused cursor is drawn solid.
func RenderInput(input *textinput.Model, sel Selection, st lipgloss.Style) string {
	runes := []rune(input.Value())
	if len(runes) == 0 && input.Placeholder != "" {
		return input.View()
	}
	focused := input.Focused()
	pos := clamp(input.Position(), len(runes))
	start, end := sel.Range()
	start, end = clamp(start, len(runes)), clamp(end, len(runes))

	// Width 0 means no window: the whole value is shown, and the caller's pane
	// clips it. Real fields always set Width, so the cursor stays visible.
	offset := 0
	if w := input.Width; w > 0 && pos >= w {
		// Keep the cursor cell inside the w-cell window.
		offset = pos - w + 1
	}
	visibleEnd := len(runes)
	if w := input.Width; w > 0 {
		visibleEnd = min(len(runes), offset+w)
	}

	var b strings.Builder
	b.WriteString(input.PromptStyle.Render(input.Prompt))
	for i := offset; i < visibleEnd; i++ {
		r := string(runes[i])
		switch {
		case i == pos && focused:
			b.WriteString(cursorStyle.Render(r))
		case i >= start && i < end:
			b.WriteString(st.Render(r))
		default:
			b.WriteString(input.TextStyle.Render(r))
		}
	}
	if pos == len(runes) && focused {
		// The cursor sits after the last rune and needs its own cell.
		b.WriteString(cursorStyle.Render(" "))
	}
	return b.String()
}
