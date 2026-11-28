package tui

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

const viewerDoubleClickWindow = 400

func viewerAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m Model) openViewerForResponse() Model {
	m.setResponseTextContent()
	display := ""
	if m.responseText.cache != nil {
		display = m.responseText.cache.content
	}
	m.viewerContent = display
	m.viewerText = scrollableText{cache: &scrollableTextCache{}}
	m.viewerText.SetContent(m.viewerContent)
	m.viewerText.offset = 0
	m.viewerText.SetDebugLog(m.debugLog, "viewer")
	m.viewerText.SetTiming(m.timing)
	_, m.viewerCopy = m.viewerResponseContent()
	m.resetViewerFilter()
	m.mode = viewerMode
	m.viewerFindOpen = false
	m.viewerFind.SetValue("")
	m.viewerFind.Blur()
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	return m
}

func (m Model) openViewerForRequest() Model {
	m.setRequestTextContent()
	display := ""
	if m.requestText.cache != nil {
		display = m.requestText.cache.content
	}
	m.viewerContent = display
	m.viewerText = scrollableText{cache: &scrollableTextCache{}}
	m.viewerText.SetContent(m.viewerContent)
	m.viewerText.offset = 0
	m.viewerText.SetDebugLog(m.debugLog, "viewer")
	m.viewerText.SetTiming(m.timing)
	m.viewerCopy = m.viewerRequestContent()
	m.resetViewerFilter()
	m.mode = viewerMode
	m.viewerFindOpen = false
	m.viewerFind.SetValue("")
	m.viewerFind.Blur()
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	return m
}

func (m Model) closeViewer() Model {
	m.mode = normalMode
	m.resetViewerFilter()
	m.viewerFindOpen = false
	m.viewerFind.Blur()
	m.viewerFind.SetValue("")
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	return m
}

func (m Model) openViewerFind() (Model, tea.Cmd) {
	m.viewerFindOpen = true
	m.viewerFind.SetValue("")
	m.viewerFind.Focus()
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	return m, textinput.Blink
}

func (m Model) handleViewerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.viewerFilter.open {
		return m.handleViewerFilterKey(msg)
	}
	key := msg.String()
	keys := m.textKeys()
	if m.viewerFindOpen {
		ed := m.newTextEditor()
		b := textedit.Input(&m.viewerFind)
		if cmd, handled := ed.HandleKey(&m.viewerFindSelection, b, msg); handled {
			m.viewerFindFirstMatch()
			return m, cmd
		}
		if keys.Copy.Match(key) {
			// No selection: nothing to copy. Ctrl+C never quits the app.
			return m.ctrlCNoSelection(), nil
		}
		switch {
		case keys.ViewerClose.Match(key):
			m.viewerFindOpen = false
			m.viewerFind.Blur()
			return m, nil
		case keys.FindPrev.Match(key):
			m.moveViewerMatch(-1)
			return m, nil
		case keys.FindNext.Match(key):
			m.moveViewerMatch(1)
			return m, nil
		}
		var cmd tea.Cmd
		m.viewerFind, cmd = m.viewerFind.Update(msg)
		ed.Sync(&m.viewerFindSelection, b)
		m.viewerFindFirstMatch()
		return m, cmd
	}

	if keys.ViewerClose.Match(key) {
		return m.closeViewer(), nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		m.scrollViewer(msg)
		return m, nil
	}

	switch {
	case keys.ViewerFind.Match(key), keys.FindOpen.Match(key):
		return m.openViewerFind()
	case keys.ViewerFilter.Match(key):
		return m.openViewerFilter()
	case keys.ViewerCopy.Match(key):
		return m, copyViewerContentCmd(m.viewerCopy)
	}
	return m, nil
}

func (m *Model) scrollViewer(msg tea.KeyMsg) {
	width := max(1, m.width)
	height := max(1, m.height-1)
	switch msg.Type {
	case tea.KeyHome:
		m.viewerText.offset = 0
	case tea.KeyEnd:
		m.viewerText.Scroll(1<<30, width, height)
	default:
		delta := -1
		if msg.Type == tea.KeyDown || msg.Type == tea.KeyPgDown {
			delta = 1
		}
		if msg.Type == tea.KeyPgUp || msg.Type == tea.KeyPgDown {
			delta *= max(1, height-1)
		}
		m.viewerText.Scroll(delta, width, height)
	}
}

func (m *Model) viewerFindFirstMatch() {
	query := strings.ToLower(strings.TrimSpace(m.viewerFind.Value()))
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	m.viewerText.SetContent(m.viewerContent)
	if query == "" || m.viewerText.cache == nil {
		return
	}
	lines := m.viewerText.lines(max(1, m.width))
	for i, line := range lines {
		for range countFoldMatches(stripANSI(line), query) {
			m.viewerMatches = append(m.viewerMatches, i)
		}
	}
	if len(m.viewerMatches) > 0 {
		m.viewerLastMatch = 0
		m.viewerText.offset = m.viewerMatches[0]
	}
}

func countFoldMatches(text, query string) int {
	textRunes := []rune(text)
	queryRunes := []rune(query)
	if len(queryRunes) == 0 {
		return 0
	}
	count := 0
	for i := 0; i+len(queryRunes) <= len(textRunes); i++ {
		if strings.EqualFold(string(textRunes[i:i+len(queryRunes)]), query) {
			count++
			i += len(queryRunes) - 1
		}
	}
	return count
}

func (m *Model) moveViewerMatch(direction int) {
	if len(m.viewerMatches) == 0 {
		return
	}
	m.viewerLastMatch = (m.viewerLastMatch + direction) % len(m.viewerMatches)
	if m.viewerLastMatch < 0 {
		m.viewerLastMatch += len(m.viewerMatches)
	}
	m.viewerText.offset = m.viewerMatches[m.viewerLastMatch]
}

// viewerRequestContent returns the text copied from the request viewer.
func (m Model) viewerRequestContent() string {
	if m.activeRequest == nil {
		return ""
	}
	if m.activeRequest.Body != "" {
		return stripANSI(m.activeRequest.Body)
	}
	return stripANSI(m.requestBodyPreviewContent())
}

func (m Model) viewerResponseContent() (string, string) {
	display := ""
	if m.viewerText.cache != nil {
		display = m.viewerText.cache.content
	}
	if ex := m.selectedExecution(); ex != nil {
		if m.responseTab == bodyTab || m.responseTab == rawTab {
			return display, stripANSI(ex.ResponseBody)
		}
		return display, stripANSI(display)
	}
	if m.response != nil && (m.responseTab == bodyTab || m.responseTab == rawTab) {
		if m.response.Body == nil && m.response.TempPath != "" {
			if body, err := os.ReadFile(m.response.TempPath); err == nil {
				return display, stripANSI(string(body))
			}
		}
		return display, stripANSI(string(m.response.Body))
	}
	return display, stripANSI(display)
}

func copyViewerContentCmd(content string) tea.Cmd {
	return func() tea.Msg {
		return viewerClipboardMsg{err: clipboard.WriteAll(content)}
	}
}

func copyCurlCommandCmd(content string) tea.Cmd {
	return func() tea.Msg {
		return curlExportClipboardMsg{err: clipboard.WriteAll(content)}
	}
}

func (m Model) viewViewer() string {
	keys := m.textKeys()
	help := "[" + keys.ViewerFind[0] + "] find  [" + keys.ViewerFilter[0] + "] jq filter  [" +
		keys.ViewerCopy[0] + "] copy body  [" +
		strings.Join(keys.ViewerClose, "/") + "] close  [shift+click] terminal select"
	matchStatus := m.viewerMatchStatus()
	switch {
	case m.viewerFilter.open:
		help = "jq: " + m.viewerFilter.input.View() + m.viewerFilterStatus() +
			"  [enter] keep  [" + strings.Join(keys.ViewerClose, "/") + "] clear"
	case m.viewerFindOpen:
		help = "Find: " + m.viewerFind.View() + matchStatus + "  [" + keys.FindNext[0] +
			"] next  [" + keys.FindPrev[0] + "] previous  [" + strings.Join(keys.ViewerClose, "/") + "] close"
	default:
		help += matchStatus
	}
	if !m.viewerFilter.open {
		// The filter status leads the footer so a narrow terminal keeps it.
		if status := m.viewerFilterStatus(); status != "" {
			help = strings.TrimPrefix(status, "  ") + "  " + help
		}
	}
	help = truncate(stripANSI(help), max(1, m.width))
	help = m.styles.mutedStyle.Render(help)
	textHeight := max(1, m.height-1)
	body := m.viewerText.View(max(1, m.width), textHeight)
	if query := strings.TrimSpace(m.viewerFind.Value()); query != "" {
		body = highlightViewerMatches(body, query)
	}
	body = lipgloss.NewStyle().Width(max(1, m.width)).Height(textHeight).Render(body)
	out := lipgloss.JoinVertical(lipgloss.Left, body, help)
	return clipToRows(out, m.width, m.height)
}

func (m Model) viewerMatchStatus() string {
	if strings.TrimSpace(m.viewerFind.Value()) == "" {
		return ""
	}
	if len(m.viewerMatches) == 0 {
		return "  match 0/0"
	}
	return fmt.Sprintf("  match %d/%d", m.viewerLastMatch+1, len(m.viewerMatches))
}

func highlightViewerMatches(content, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return content
	}
	// Reverse video keeps matches readable on either terminal background and
	// retains any foreground syntax color beneath the temporary marker.
	const matchStart = "\x1b[7m\x1b[1m"
	const matchEnd = "\x1b[27m\x1b[22m"
	contentRunes := []rune(content)
	queryRunes := []rune(query)
	if len(queryRunes) == 0 {
		return content
	}
	// Walk the original ANSI stream while matching only visible runes. This
	// preserves the syntax colors emitted by the parent scrollable component.
	var out strings.Builder
	visible := make([]rune, 0, len(contentRunes))
	for i := 0; i < len(content); {
		if match := ansiEscape.FindStringIndex(content[i:]); match != nil && match[0] == 0 {
			out.WriteString(content[i : i+match[1]])
			i += match[1]
			continue
		}
		r, size := utf8.DecodeRuneInString(content[i:])
		visible = append(visible, r)
		i += size
	}

	matches := make(map[int]bool)
	for i := 0; i+len(queryRunes) <= len(visible); i++ {
		if strings.EqualFold(string(visible[i:i+len(queryRunes)]), query) {
			for j := i; j < i+len(queryRunes); j++ {
				matches[j] = true
			}
		}
	}
	if len(matches) == 0 {
		return content
	}

	// Re-scan the stream, inserting only background/bold controls so existing
	// foreground syntax colors remain active underneath the match highlight.
	out.Reset()
	visibleIndex := 0
	matchOpen := false
	for i := 0; i < len(content); {
		if match := ansiEscape.FindStringIndex(content[i:]); match != nil && match[0] == 0 {
			out.WriteString(content[i : i+match[1]])
			i += match[1]
			continue
		}
		r, size := utf8.DecodeRuneInString(content[i:])
		shouldHighlight := matches[visibleIndex]
		if shouldHighlight && !matchOpen {
			out.WriteString(matchStart)
			matchOpen = true
		} else if !shouldHighlight && matchOpen {
			out.WriteString(matchEnd)
			matchOpen = false
		}
		out.WriteRune(r)
		visibleIndex++
		i += size
	}
	if matchOpen {
		out.WriteString(matchEnd)
	}
	return out.String()
}
