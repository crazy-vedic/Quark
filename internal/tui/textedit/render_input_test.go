package textedit

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// newField returns a focused field, since most cases check the cursor cell.
func newField(width int, value string) *textinput.Model {
	ti := textinput.New()
	ti.Width = width
	ti.SetValue(value)
	ti.Focus()
	return &ti
}

func TestRenderInputLongValueScrollsToCursor(t *testing.T) {
	ti := newField(10, "abcdefghijklmnopqrstuvwxyz")
	out := RenderInput(ti, Selection{Anchor: 26, Cursor: 26}, lipgloss.NewStyle())
	plain := ansi.Strip(out)
	// Prompt "> " plus a 10-cell window: 9 text cells and the cursor cell.
	assert.Equal(t, 2+10, lipgloss.Width(plain))
	assert.Contains(t, plain, "rstuvwxyz")
	assert.NotContains(t, plain, "abc")
}

func TestRenderInputKeepsCursorAtEndOfSelection(t *testing.T) {
	forceColor(t)
	ti := newField(0, "hello world")
	ti.SetCursor(3)
	sel := Selection{Anchor: 0, Cursor: 3}
	out := RenderInput(ti, sel, lipgloss.NewStyle().Underline(true))
	assert.Equal(t, "> hello world", ansi.Strip(out))
	// The cursor cell is reversed, so the escape before "l" is present.
	assert.Contains(t, out, "\x1b[7m")
}

func TestRenderInputPlaceholderWhenEmpty(t *testing.T) {
	ti := textinput.New()
	ti.Placeholder = "https://example.com"
	out := RenderInput(&ti, Selection{}, lipgloss.NewStyle())
	assert.Contains(t, ansi.Strip(out), "https://example.com")
}

func TestRenderInputSelectionKeepsTextAndStyle(t *testing.T) {
	forceColor(t)
	ti := newField(0, "abcdef")
	ti.SetCursor(4)
	st := lipgloss.NewStyle().Underline(true)
	out := RenderInput(ti, Selection{Anchor: 1, Cursor: 4}, st)
	assert.Equal(t, "> abcdef", ansi.Strip(out))
	// "bcd" is selected, so each selected rune carries the selection style.
	assert.Contains(t, out, st.Render("b"))
	assert.Contains(t, out, st.Render("d"))
	assert.NotContains(t, out, st.Render("e"))
}

// A field with Width 0 has no window: it renders the whole value, keeps the
// cursor cell and draws the selection. The header and auth inputs are unset,
// so this is their normal path.
func TestRenderInputWithoutWidthRendersWholeValue(t *testing.T) {
	ti := newField(0, "abcdefghijklmnopqrstuvwxyz")
	ti.SetCursor(20)
	sel := Selection{Anchor: 2, Cursor: 5}
	out := RenderInput(ti, sel, lipgloss.NewStyle())
	assert.Equal(t, "> abcdefghijklmnopqrstuvwxyz", ansi.Strip(out))
}

// An unfocused field must not draw a cursor. The header and env editors render
// both Key and Value through RenderInput, so only the focused one shows it.
func TestRenderInputUnfocusedDrawsNoCursor(t *testing.T) {
	forceColor(t)
	ti := newField(0, "hello")
	ti.SetCursor(2)
	ti.Blur()
	assert.False(t, ti.Focused())
	out := RenderInput(ti, Selection{}, lipgloss.NewStyle())
	assert.Equal(t, "> hello", ansi.Strip(out))
	assert.NotContains(t, out, "\x1b[7m")

	ti.Focus()
	out = RenderInput(ti, Selection{}, lipgloss.NewStyle())
	assert.Contains(t, out, "\x1b[7m")
}

// The cursor must not pin to the right edge: text after it stays visible so
// the user can see what follows the cursor.
func TestRenderInputShowsContextRightOfCursor(t *testing.T) {
	ti := newField(5, "0123456789")
	ti.SetCursor(7)
	out := RenderInput(ti, Selection{Anchor: 7, Cursor: 7}, lipgloss.NewStyle())
	plain := ansi.Strip(out)
	assert.Contains(t, plain, "7")
	assert.Contains(t, plain, "89")
}

// Wide runes take two cells each, so the window must be measured in display
// cells, not runes. A width-5 field with CJK text stays within 5 cells after
// the prompt for every cursor position.
func TestRenderInputWideRunesStayWithinWidth(t *testing.T) {
	for _, pos := range []int{0, 2, 4, 6} {
		ti := newField(5, "中文字符测试")
		ti.SetCursor(pos)
		out := RenderInput(ti, Selection{Anchor: pos, Cursor: pos}, lipgloss.NewStyle())
		plain := ansi.Strip(out)
		assert.LessOrEqual(t, lipgloss.Width(plain), 2+5, "cursor at %d: %q", pos, plain)
	}
}
