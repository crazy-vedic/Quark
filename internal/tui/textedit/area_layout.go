package textedit

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
)

// visualRow is one wrapped row of the text as [start, end) rune offsets.
type visualRow struct{ start, end int }

// visualRows lists every wrapped row of the textarea's value. It walks a
// scratch textarea configured like ta, so the real widget's viewport and
// cursor are never touched.
func visualRows(ta *textarea.Model) []visualRow {
	text := ta.Value()
	runes := []rune(text)

	probe := textarea.New()
	probe.Prompt = ta.Prompt
	probe.ShowLineNumbers = ta.ShowLineNumbers
	probe.SetWidth(ta.Width() + frameOverhead(ta))
	probe.SetValue(text)

	limit := len(runes) + strings.Count(text, "\n") + 8
	for i := 0; i < limit && (probe.Line() > 0 || probe.LineInfo().RowOffset > 0); i++ {
		probe.CursorUp()
	}

	starts := []int{rowStart(runes, &probe)}
	for i := 0; i < limit; i++ {
		prevLine, prevCol := probe.Line(), probe.LineInfo().StartColumn
		probe.CursorDown()
		if probe.Line() == prevLine && probe.LineInfo().StartColumn == prevCol {
			break // bottom of the text: CursorDown no longer moves
		}
		starts = append(starts, rowStart(runes, &probe))
	}

	rows := make([]visualRow, len(starts))
	for i, s := range starts {
		lineEnd := s
		for lineEnd < len(runes) && runes[lineEnd] != '\n' {
			lineEnd++
		}
		end := lineEnd
		if i+1 < len(starts) && starts[i+1] < end {
			end = starts[i+1]
		}
		rows[i] = visualRow{start: s, end: end}
	}
	return rows
}

func rowStart(runes []rune, ta *textarea.Model) int {
	return lineStartOffset(runes, ta.Line()) + ta.LineInfo().StartColumn
}

// frameOverhead is the number of columns the textarea uses for its prompt and
// line numbers. Width() reports the text width, so SetWidth needs this added
// back to wrap at the same column as the real widget.
func frameOverhead(ta *textarea.Model) int {
	const probeWidth = 100
	p := textarea.New()
	p.Prompt = ta.Prompt
	p.ShowLineNumbers = ta.ShowLineNumbers
	p.SetWidth(probeWidth)
	return probeWidth - p.Width()
}
