package textedit

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// RenderInput returns a single-line widget's view. With no selection it is the
// widget's own view. With one, the selected runes are drawn in st and the rest
// of the text is plain.
func RenderInput(input *textinput.Model, sel Selection, st lipgloss.Style) string {
	if !sel.Active() {
		return input.View()
	}
	runes := []rune(input.Value())
	start, end := sel.Range()
	end = clamp(end, len(runes))
	start = clamp(start, end)

	text := string(runes[:start])
	if start < end {
		text += st.Render(string(runes[start:end]))
	}
	return text + string(runes[end:])
}
