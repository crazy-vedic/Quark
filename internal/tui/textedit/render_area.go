package textedit

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// RenderArea overlays the selection on the output of ta.View(). gutter is the
// number of columns the textarea draws before the text (prompt and line
// numbers). The textarea does not expose its scroll offset, so the visible
// window is found by matching the view's rows against the wrapped rows. If no
// window matches, the view is returned unchanged rather than drawn wrong.
func RenderArea(
	view string,
	ta *textarea.Model,
	gutter int,
	sel Selection,
	st lipgloss.Style,
) string {
	if !sel.Active() || view == "" {
		return view
	}
	runes := []rune(ta.Value())
	rows := visualRows(ta)
	if len(rows) == 0 {
		return view
	}
	cur := AreaBackend{m: ta}.Cursor()
	curRow := rowIndexAt(rows, cur)
	lines := strings.Split(view, "\n")

	top, ok := findWindowTop(rows, runes, lines, gutter, curRow)
	if !ok {
		return view
	}

	s, e := sel.Range()
	for k := range lines {
		idx := top + k
		if idx >= len(rows) {
			break
		}
		r := rows[idx]
		os, oe := max(s, r.start), min(e, r.end)
		if os >= oe {
			continue
		}
		lines[k] = overlayRow(lines[k], gutter, runes[r.start:r.end], os-r.start, oe-r.start, st)
	}
	return strings.Join(lines, "\n")
}

// rowIndexAt returns the index of the row containing offset pos.
func rowIndexAt(rows []visualRow, pos int) int {
	idx := 0
	for i, r := range rows {
		if r.start <= pos {
			idx = i
		}
	}
	return idx
}

// findWindowTop returns the first row index shown at the top of the view. The
// cursor row must be inside the window, so only tops within one view height of
// it are tried.
func findWindowTop(rows []visualRow, runes []rune, lines []string, gutter, curRow int) (int, bool) {
	for top := curRow; top >= 0 && top > curRow-len(lines); top-- {
		if windowMatches(rows, runes, lines, gutter, top) {
			return top, true
		}
	}
	return 0, false
}

func windowMatches(rows []visualRow, runes []rune, lines []string, gutter, top int) bool {
	for k, line := range lines {
		idx := top + k
		got := strings.TrimRight(ansi.Strip(ansi.Cut(line, gutter, lipgloss.Width(line))), " ")
		want := ""
		if idx < len(rows) {
			r := rows[idx]
			want = strings.TrimRight(string(runes[r.start:r.end]), " ")
		}
		if got != want {
			return false
		}
	}
	return true
}

// overlayRow styles columns of the rendered line that hold row runes [a, b).
func overlayRow(line string, gutter int, row []rune, a, b int, st lipgloss.Style) string {
	startCol := gutter + lipgloss.Width(string(row[:a]))
	endCol := gutter + lipgloss.Width(string(row[:b]))
	w := lipgloss.Width(line)
	left := ansi.Cut(line, 0, startCol)
	mid := ansi.Strip(ansi.Cut(line, startCol, endCol))
	right := ansi.Cut(line, endCol, w)
	return left + st.Render(mid) + right
}
