package textedit

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/textarea"
)

// visualRow is one wrapped row of the text as [start, end) rune offsets.
type visualRow struct{ start, end int }

// rowLayoutBuilds counts full layout walks; tests use it to check the cache.
var rowLayoutBuilds atomic.Int64

// rowLayoutKey is everything the wrapped layout depends on.
type rowLayoutKey struct {
	text       string
	width      int
	prompt     string
	lineNumber bool
}

// rowCache holds the layout of the last text laid out. Each frame calls
// visualRows again, so one entry is enough to skip the walk while the body
// is unchanged.
var rowCache struct {
	sync.Mutex
	key  rowLayoutKey
	rows []visualRow
	ok   bool
}

// resetRowLayoutCache drops any cached layout; tests use it for a clean count.
func resetRowLayoutCache() {
	rowCache.Lock()
	defer rowCache.Unlock()
	rowCache.ok = false
	rowCache.rows = nil
}

// visualRows lists every wrapped row of the textarea's value, reusing the last
// layout when the text, width and frame are unchanged.
func visualRows(ta *textarea.Model) []visualRow {
	key := rowLayoutKey{
		text:       ta.Value(),
		width:      ta.Width(),
		prompt:     ta.Prompt,
		lineNumber: ta.ShowLineNumbers,
	}
	rowCache.Lock()
	defer rowCache.Unlock()
	if rowCache.ok && rowCache.key == key {
		return rowCache.rows
	}
	rows := buildVisualRows(ta, key.text)
	rowCache.key, rowCache.rows, rowCache.ok = key, rows, true
	return rows
}

// buildVisualRows walks a scratch textarea configured like ta, so the real
// widget's viewport and cursor are never touched.
func buildVisualRows(ta *textarea.Model, text string) []visualRow {
	rowLayoutBuilds.Add(1)
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
