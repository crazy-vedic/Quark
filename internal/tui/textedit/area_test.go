package textedit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
)

// forceColor makes lipgloss emit escape codes in tests; without a TTY it
// renders plain text and styling is invisible.
func forceColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// newArea builds a textarea like the body editor: thick prompt, line numbers
// (gutter 2+4 = 6 columns), cursor at the end of the value.
func newArea(width, height int, value string) *textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = true
	ta.SetWidth(width)
	ta.SetHeight(height)
	ta.SetValue(value)
	return &ta
}

const bodyGutter = 6

func TestAreaCursorAcrossLinesAndWraps(t *testing.T) {
	ta := newArea(20, 10, strings.Repeat("x", 45)+"\nshort")
	b := Area(ta)
	b.SetCursor(25)
	assert.Equal(t, 25, b.Cursor())
	b.SetCursor(46) // start of "short"
	assert.Equal(t, 46, b.Cursor())
	b.SetCursor(0)
	assert.Equal(t, 0, b.Cursor())
}

func TestAreaSetCursorDoesNotDependOnStart(t *testing.T) {
	ta := newArea(10, 20, strings.Repeat("a", 40)+"\nbb\ncc")
	b := Area(ta)
	b.SetCursor(42) // "bb" col 1
	assert.Equal(t, 42, b.Cursor())
	b.SetCursor(5)
	assert.Equal(t, 5, b.Cursor())
}

func TestVisualRowsMatchWrap(t *testing.T) {
	ta := newArea(20, 20, strings.Repeat("a", 45)+"\nbb")
	rows := visualRows(ta)
	// Width 20 with a 6-column gutter gives 14 text columns per row:
	// 45 chars -> rows of 14, 14, 14, 3; then "bb" starts at offset 46.
	assert.Len(t, rows, 5)
	assert.Equal(t, visualRow{0, 14}, rows[0])
	assert.Equal(t, visualRow{42, 45}, rows[3])
	assert.Equal(t, visualRow{46, 48}, rows[4])
}

func TestVisualRowsLeaveRealWidgetUntouched(t *testing.T) {
	ta := newArea(10, 20, strings.Repeat("a", 40)+"\nbb")
	before := Area(ta).Cursor()
	_ = visualRows(ta)
	assert.Equal(t, before, Area(ta).Cursor())
}

func TestRenderAreaNoSelectionReturnsViewUnchanged(t *testing.T) {
	ta := newArea(20, 5, "hello")
	view := ta.View()
	var sel Selection
	sel.Collapse(0)
	assert.Equal(t, view, RenderArea(view, ta, bodyGutter, sel, lipgloss.NewStyle()))
}

func TestRenderAreaKeepsRowWidthsAndText(t *testing.T) {
	forceColor(t)
	ta := newArea(20, 10, strings.Repeat("x", 45)+"\nshort")
	ta.Focus()
	view := ta.View()
	var sel Selection
	sel.SelectRange(3, 12) // spans the first wrapped row and into the next
	out := RenderArea(view, ta, bodyGutter, sel, lipgloss.NewStyle().Reverse(true))

	in := strings.Split(view, "\n")
	got := strings.Split(out, "\n")
	assert.Equal(t, len(in), len(got))
	for i := range in {
		assert.Equal(t, lipgloss.Width(in[i]), lipgloss.Width(got[i]), "row %d width", i)
		assert.Equal(t, ansi.Strip(in[i]), ansi.Strip(got[i]), "row %d text", i)
	}
	assert.NotEqual(t, view, out, "selection must change the view")
}

func TestRenderAreaFindsWindowWhenScrolled(t *testing.T) {
	forceColor(t)
	// 12 rows of text in a 3-row viewport; the cursor is at the end, so the
	// view is scrolled to the bottom.
	var lines []string
	for i := 0; i < 12; i++ {
		lines = append(lines, "line"+string(rune('A'+i)))
	}
	ta := newArea(20, 3, strings.Join(lines, "\n"))
	ta.Focus()
	// Move the cursor the way the app does; only key handling scrolls the view
	// to the cursor.
	for i := 0; i < 12; i++ {
		ta.CursorUp()
	}
	for i := 0; i < 12; i++ {
		*ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyDown})
		_ = ta.View() // rendering is what scrolls the viewport; the app renders each frame
	}
	view := ta.View()
	assert.Contains(t, ansi.Strip(view), "lineL", "precondition: view scrolled to the cursor")
	var sel Selection
	sel.SelectRange(0, 4) // "lineA", which is scrolled out of view
	out := RenderArea(view, ta, bodyGutter, sel, lipgloss.NewStyle().Reverse(true))
	assert.Equal(t, view, out, "selection above the window draws nothing")

	var sel2 Selection
	last := len([]rune(strings.Join(lines, "\n")))
	sel2.SelectRange(last-5, last) // "lineL", visible
	out2 := RenderArea(view, ta, bodyGutter, sel2, lipgloss.NewStyle().Reverse(true))
	assert.NotEqual(t, view, out2)
	first := strings.Split(view, "\n")[0]
	firstOut := strings.Split(out2, "\n")[0]
	assert.Equal(t, lipgloss.Width(first), lipgloss.Width(firstOut))
}

func TestAreaShiftDownExtendsAcrossRows(t *testing.T) {
	ta := newArea(20, 10, "abc\ndef\nghi")
	b := Area(ta)
	b.SetCursor(1) // "abc" col 1
	var ed = New(nopClipboard{})
	var sel Selection
	sel.Collapse(1)
	ed.HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyShiftDown})
	assert.Equal(t, 5, sel.Cursor) // "def" col 1
	s, e := sel.Range()
	assert.Equal(t, [2]int{1, 5}, [2]int{s, e})
}

func TestAreaShiftHomeStopsAtLineStart(t *testing.T) {
	ta := newArea(20, 10, "abc\ndef")
	b := Area(ta)
	b.SetCursor(6) // "def" col 2
	var sel Selection
	sel.Collapse(6)
	New(nopClipboard{}).HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyShiftHome})
	s, e := sel.Range()
	assert.Equal(t, [2]int{4, 6}, [2]int{s, e})
}

func TestAreaEnterReplacesSelectionWithNewline(t *testing.T) {
	ta := newArea(20, 10, "abcdef")
	ta.Focus()
	b := Area(ta)
	var sel Selection
	sel.SelectRange(1, 3)
	New(nopClipboard{}).HandleKey(&sel, b, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "a\ndef", ta.Value())
}

// wordKeyMsgs maps the word-movement key strings in DefaultKeymap to messages.
// Keys are built from the bubbletea types so String() matches the keymap.
var wordKeyMsgs = map[string]tea.KeyMsg{
	"ctrl+left":  {Type: tea.KeyCtrlLeft},
	"alt+left":   {Type: tea.KeyLeft, Alt: true},
	"ctrl+right": {Type: tea.KeyCtrlRight},
	"alt+right":  {Type: tea.KeyRight, Alt: true},
}

// TestWordKeysMoveRealTextarea drives every word-movement key in the keymap
// through the real bubbles textarea. The textarea's own keymap does not bind
// ctrl+left, so this fails if movement is delegated to the widget again.
func TestWordKeysMoveRealTextarea(t *testing.T) {
	ed := New(nopClipboard{})
	for _, key := range append(append([]string{}, ed.Keys.WordLeft...), ed.Keys.WordRight...) {
		msg, ok := wordKeyMsgs[key]
		if !assert.True(t, ok, "no test message for %q", key) {
			continue
		}
		assert.Equal(t, key, msg.String())
		ta := newArea(40, 5, "foo bar baz")
		b := Area(ta)
		var sel Selection
		sel.Collapse(11)
		b.SetCursor(11)
		ed.HandleKey(&sel, b, msg)
		if ed.Keys.WordLeft.Match(key) {
			assert.Equal(t, 8, sel.Cursor, key)
		} else {
			assert.Equal(t, 11, sel.Cursor, key) // already at end: stays put
		}
		assert.Equal(t, sel.Cursor, b.Cursor(), key)
	}
}

func TestWordLeftMovesFromMiddleInRealTextarea(t *testing.T) {
	ed := New(nopClipboard{})
	ta := newArea(40, 5, "foo bar baz")
	b := Area(ta)
	var sel Selection
	sel.Collapse(10)
	b.SetCursor(10)
	ed.HandleKey(&sel, b, wordKeyMsgs["ctrl+left"])
	assert.Equal(t, 8, sel.Cursor)
	assert.Equal(t, 8, b.Cursor())
	ed.HandleKey(&sel, b, wordKeyMsgs["ctrl+right"])
	assert.Equal(t, 11, sel.Cursor) // skips "baz" to the end
}

// TestAreaSetCursorLargeBody checks moves on a 10,000-line body in both
// directions. The result must be exact; the walk is bounded by the distance.
func TestAreaSetCursorLargeBody(t *testing.T) {
	lines := make([]string, 10000)
	for i := range lines {
		lines[i] = "line"
	}
	value := strings.Join(lines, "\n")
	ta := newArea(40, 10, value)
	b := Area(ta)
	// Offset of line 9000, column 2: 9000 lines of "line\n" (5 runes each).
	target := 9000*5 + 2
	b.SetCursor(target)
	assert.Equal(t, target, b.Cursor())
	b.SetCursor(12) // line 2, col 2: walk back up
	assert.Equal(t, 12, b.Cursor())
}
