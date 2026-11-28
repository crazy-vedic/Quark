package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// historyMarkLimit is how many history entries can be marked for a comparison.
const historyMarkLimit = 2

// maxDiffCells bounds the line-diff table. Larger bodies are shown as one
// removed block and one added block instead of an aligned diff.
const maxDiffCells = 4_000_000

// historyDiffLoadedMsg carries the two marked entries with their bodies. first
// was marked first, so it is A (removed lines) and second is B (added lines).
type historyDiffLoadedMsg struct {
	requestID string
	first     *domain.Execution
	second    *domain.Execution
}

// toggleHistoryMark marks the selected history entry, or unmarks it when it is
// already marked. Marks are kept in the order they were made: the first is A.
func (m Model) toggleHistoryMark() Model {
	ex := m.selectedExecution()
	if ex == nil {
		return m
	}
	for i, id := range m.historyMarks {
		if id == ex.ID {
			m.historyMarks = append(m.historyMarks[:i:i], m.historyMarks[i+1:]...)
			return m.status("success", "Unmarked history entry")
		}
	}
	if len(m.historyMarks) >= historyMarkLimit {
		return m.status("warn", "Two history entries are marked; unmark one first (m)")
	}
	m.historyMarks = append(m.historyMarks, ex.ID)
	if len(m.historyMarks) < historyMarkLimit {
		return m.status("success", "Marked history entry as A; mark one more to compare")
	}
	return m.status("success", "Marked history entry as B; press D to compare")
}

// historyMarkLetter returns "A" or "B" for a marked entry, or "" otherwise.
func (m Model) historyMarkLetter(id string) string {
	for i, marked := range m.historyMarks {
		if marked == id {
			return string(rune('A' + i))
		}
	}
	return ""
}

// pruneHistoryMarks keeps only the marks whose entries are still listed.
func pruneHistoryMarks(marks []string, executions []*domain.Execution) []string {
	var kept []string
	for _, id := range marks {
		for _, ex := range executions {
			if ex.ID == id {
				kept = append(kept, id)
				break
			}
		}
	}
	return kept
}

// executionHistoryPopupVisible reports whether the history popup is shown. It
// shows while browsing older entries, and while any entry is marked so the
// marks stay visible even on the latest entry.
func (m Model) executionHistoryPopupVisible() bool {
	return m.viewingHistoricalExecution() ||
		(len(m.executions) > 1 && len(m.historyMarks) > 0)
}

// compareMarkedHistory loads both marked entries with their bodies and opens
// their comparison.
func (m Model) compareMarkedHistory() (Model, tea.Cmd) {
	if len(m.historyMarks) != historyMarkLimit {
		return m.status("warn", "Mark two history entries with m, then press D to compare"), nil
	}
	if m.executionReader == nil || m.activeRequest == nil {
		return m.status("error", "History is unavailable"), nil
	}
	return m, loadHistoryDiffCmd(
		m.ctx, m.executionReader, m.activeRequest.ID, m.historyMarks[0], m.historyMarks[1],
	)
}

func loadHistoryDiffCmd(
	ctx context.Context,
	reader store.ExecutionReader,
	requestID, firstID, secondID string,
) tea.Cmd {
	return func() tea.Msg {
		first, err := reader.GetExecution(ctx, firstID)
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("load execution body: %w", err)}
		}
		second, err := reader.GetExecution(ctx, secondID)
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("load execution body: %w", err)}
		}
		return historyDiffLoadedMsg{requestID: requestID, first: first, second: second}
	}
}

// historyDiffText compares the status, headers and body of two runs. Lines
// prefixed "- " are only in a (A), lines prefixed "+ " only in b (B), and
// lines prefixed "  " are the same in both.
func historyDiffText(a, b *domain.Execution) string {
	var sb strings.Builder
	sb.WriteString("Compare history entries\n\n")
	fmt.Fprintf(&sb, "  - A  %s  %s\n", historyEntryTime(a), a.ID)
	fmt.Fprintf(&sb, "  + B  %s  %s\n", historyEntryTime(b), b.ID)

	writeSection := func(title string, lines []string, changed bool) {
		status := "same"
		if changed {
			status = "differs"
		}
		fmt.Fprintf(&sb, "\n%s (%s)\n", title, status)
		for _, line := range lines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	statusLines, statusChanged := diffLines(
		statusDiffLines(a), statusDiffLines(b),
	)
	writeSection("Status", statusLines, statusChanged)

	headerLines, headersChanged := diffLines(headerDiffLines(a), headerDiffLines(b))
	writeSection("Headers", headerLines, headersChanged)

	bodyA, bodyB := bodyDiffLines(a), bodyDiffLines(b)
	if len(bodyA) == 0 && len(bodyB) == 0 {
		writeSection("Body", []string{"  (no stored body in either entry)"}, false)
	} else {
		bodyLines, bodyChanged := diffLines(bodyA, bodyB)
		writeSection("Body", bodyLines, bodyChanged)
	}
	return sb.String()
}

func historyEntryTime(ex *domain.Execution) string {
	return ex.CompletedAt.In(time.Local).Format("2006-01-02 15:04:05")
}

func statusDiffLines(ex *domain.Execution) []string {
	lines := []string{fmt.Sprintf("%d", ex.StatusCode)}
	if ex.StatusCode == 0 {
		lines[0] = "no response"
	}
	if ex.Error != "" {
		lines = append(lines, "error: "+stripANSI(strings.Join(strings.Fields(ex.Error), " ")))
	}
	return lines
}

func headerDiffLines(ex *domain.Execution) []string {
	var headers http.Header
	if ex.ResponseHeaders == "" || json.Unmarshal([]byte(ex.ResponseHeaders), &headers) != nil {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sortHeaderKeys(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, stripANSI(key+": "+strings.Join(headers[key], ", ")))
	}
	return lines
}

// bodyDiffLines splits a stored body into lines. JSON is re-indented first so
// that equal documents with different whitespace compare equal line by line.
func bodyDiffLines(ex *domain.Execution) []string {
	body := ex.ResponseBody
	if body == "" {
		return nil
	}
	var pretty bytes.Buffer
	if json.Valid([]byte(body)) && json.Indent(&pretty, []byte(body), "", "  ") == nil {
		body = pretty.String()
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i, line := range lines {
		lines[i] = stripANSI(line)
	}
	if ex.ResponseBodyTruncated {
		lines = append(lines, fmt.Sprintf(
			"[stored first %d of %d bytes]", len(ex.ResponseBody), ex.ResponseSize,
		))
	}
	return lines
}

// diffLines aligns a and b by their longest common subsequence of lines. It
// returns the merged lines with "  ", "- " and "+ " prefixes, and whether any
// line differs.
func diffLines(a, b []string) ([]string, bool) {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix &&
		a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}

	out := make([]string, 0, len(a)+len(b))
	for _, line := range a[:prefix] {
		out = append(out, "  "+line)
	}
	middle := alignLines(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix])
	out = append(out, middle...)
	for _, line := range a[len(a)-suffix:] {
		out = append(out, "  "+line)
	}

	changed := false
	for _, line := range out {
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "+ ") {
			changed = true
			break
		}
	}
	return out, changed
}

func alignLines(a, b []string) []string {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if n*m > maxDiffCells {
		out := make([]string, 0, n+m+1)
		out = append(out, fmt.Sprintf("  [too large to align: %d and %d lines differ]", n, m))
		for _, line := range a {
			out = append(out, "- "+line)
		}
		for _, line := range b {
			out = append(out, "+ "+line)
		}
		return out
	}

	// lcs[i][j] is the length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, "  "+a[i])
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "- "+a[i])
			i++
		default:
			out = append(out, "+ "+b[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "- "+a[i])
	}
	for ; j < m; j++ {
		out = append(out, "+ "+b[j])
	}
	return out
}

// openViewerForText opens the scrollable viewer on text the caller built.
func (m Model) openViewerForText(text string) Model {
	m.viewerContent = text
	m.viewerText = scrollableText{cache: &scrollableTextCache{}}
	m.viewerText.SetContent(text)
	m.viewerText.offset = 0
	m.viewerText.SetDebugLog(m.debugLog, "viewer")
	m.viewerText.SetTiming(m.timing)
	m.viewerCopy = text
	m.mode = viewerMode
	m.viewerFindOpen = false
	m.viewerFind.SetValue("")
	m.viewerFind.Blur()
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	return m
}
