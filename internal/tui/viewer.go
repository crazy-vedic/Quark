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
)

const viewerDoubleClickWindow = 400

func viewerAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m Model) openViewerForResponse() (Model, tea.Cmd) {
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
	m.mode = viewerMode
	m.viewerFindOpen = false
	m.viewerFind.SetValue("")
	m.viewerFind.Blur()
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	return m, nil
}

func (m Model) openViewerForRequest() (Model, tea.Cmd) {
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
	_, m.viewerCopy = m.viewerRequestContent()
	m.mode = viewerMode
	m.viewerFindOpen = false
	m.viewerFind.SetValue("")
	m.viewerFind.Blur()
	m.viewerLastMatch = -1
	m.viewerMatches = nil
	return m, nil
}

func (m Model) closeViewer() Model {
	m.mode = normalMode
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
	if m.viewerFindOpen {
		switch msg.Type {
		case tea.KeyEsc, tea.KeyTab, tea.KeyShiftTab:
			m.viewerFindOpen = false
			m.viewerFind.Blur()
			return m, nil
		case tea.KeyEnter:
			if viewerPreviousMatchKey(msg) {
				m.moveViewerMatch(-1)
			} else {
				m.moveViewerMatch(1)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.viewerFind, cmd = m.viewerFind.Update(msg)
		m.viewerFindFirstMatch()
		return m, cmd
	}

	switch msg.Type {
	case tea.KeyEsc, tea.KeyTab, tea.KeyShiftTab:
		return m.closeViewer(), nil
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		m.scrollViewer(msg)
		return m, nil
	}

	switch msg.String() {
	case "f":
		return m.openViewerFind()
	case "c":
		return m, copyViewerContentCmd(m.viewerCopy)
	case "ctrl+f", "cmd+f":
		return m.openViewerFind()
	}
	if msg.Type == tea.KeyCtrlF {
		return m.openViewerFind()
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

func viewerPreviousMatchKey(msg tea.KeyMsg) bool {
	return msg.String() == "shift+enter" || msg.String() == "shift+return" ||
		msg.String() == "alt+enter"
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

func (m Model) viewerRequestContent() (string, string) {
	if m.activeRequest == nil {
		return "", ""
	}
	display := m.requestBodyPreviewContent()
	if m.activeRequest.Body != "" {
		return display, stripANSI(m.activeRequest.Body)
	}
	return display, stripANSI(display)
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

func (m Model) viewViewer() string {
	help := "[f] find  [c] copy body  [esc/tab] close  [shift+click] terminal select"
	matchStatus := m.viewerMatchStatus()
	if m.viewerFindOpen {
		help = "Find: " + m.viewerFind.View() + matchStatus + "  [enter] next  [shift+enter] previous  [esc/tab] close"
	} else if matchStatus != "" {
		help += matchStatus
	}
	help = truncate(stripANSI(help), max(1, m.width))
	help = mutedStyle.Render(help)
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
	const matchStart = "\x1b[48;5;214m\x1b[1m"
	const matchEnd = "\x1b[49m\x1b[22m"
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
