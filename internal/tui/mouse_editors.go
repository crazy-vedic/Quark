package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	textareaPromptGutter = 2 // default thick-border prompt + space
	textareaLineNumberW  = 4 // line number column when ShowLineNumbers is true
)

type requestPaneLineLayout struct {
	contentTop      int
	contentBottom   int
	urlLineY        int
	urlTextLeft     int
	editorContentY  int
	editorContentH  int
	headerKeyInputY int
	headerValInputY int
}

func (m Model) requestPaneLineLayout(layout normalLayout) requestPaneLineLayout {
	content := layout.requestContentRect()
	topLines := 1 // title
	topLines++    // method + URL
	if m.activeRequest != nil {
		topLines++ // auth summary
	}
	topLines++ // blank separator line

	if m.activeValidationErr() != "" {
		topLines++
	}
	if m.loading {
		topLines++
	}

	chrome := m.requestPaneChromeRects(layout)
	ll := requestPaneLineLayout{
		contentTop:     content.top,
		contentBottom:  content.bottom - 2, // separator + hints
		urlLineY:       chrome.urlLine.top,
		urlTextLeft:    chrome.urlLine.left,
		editorContentY: content.top + topLines,
		editorContentH: max(1, content.bottom-2-(content.top+topLines)),
	}

	if m.activeField == headersField && m.headerEditing {
		ll.headerKeyInputY = ll.editorContentY + 1
		ll.headerValInputY = ll.editorContentY + 4
	}

	return ll
}

func (m Model) bodyTextareaTextLeft(layout normalLayout) int {
	content := layout.requestContentRect()
	return content.left + textareaPromptGutter + textareaLineNumberW
}

func (m Model) urlCursorPosFromClick(relX int, layout normalLayout) int {
	value := m.urlInput.Value()
	if m.activeField == urlField {
		return displayColumnToByteOffset(value, relX)
	}

	urlAvail := layout.mainW - 2 - methodBadgeWidth(m.method) - 4
	if urlAvail < 10 {
		urlAvail = 10
	}
	displayed := truncate(value, urlAvail)
	pos := displayColumnToByteOffset(displayed, relX)
	return min(pos, len(value))
}

func (m Model) handleURLLineClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.focus = requestPane
	layout := normalLayoutFor(m.width, m.height)
	chrome := m.requestPaneChromeRects(layout)

	relX := msg.X - chrome.urlLine.left
	if relX < 0 {
		relX = 0
	}

	var cmd tea.Cmd
	if m.activeField != urlField {
		m, cmd = m.beginURLEdit()
	} else {
		m.urlInput.Focus()
	}

	m.urlInput.SetCursor(m.urlCursorPosFromClick(relX, layout))
	if cmd == nil {
		cmd = textinput.Blink
	}
	return m, cmd
}

// handleHeaderListClick handles clicks on the header pair list (before editing
// begins). Clicking a row selects it and opens it for inline editing, mirroring
// the keyboard select-then-edit flow.
func (m Model) handleHeaderListClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if len(m.headerPairs) == 0 {
		return m, nil
	}

	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)

	idx := msg.Y - ll.editorContentY
	if idx < 0 || idx >= len(m.headerPairs) {
		return m, nil
	}

	m.headerCursor = idx
	return m.beginHeaderPairEdit(m.headerPairs[idx])
}

func (m Model) handleHeaderEditClick(msg tea.MouseMsg) (Model, tea.Cmd) {
	if !m.headerEditing {
		return m, nil
	}

	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)
	content := layout.requestContentRect()

	switch msg.Y {
	case ll.headerKeyInputY:
		relX := msg.X - content.left
		if relX < 0 {
			relX = 0
		}
		m.headerKeyInput.Focus()
		m.headerValueInput.Blur()
		m.headerKeyInput.SetCursor(displayColumnToByteOffset(m.headerKeyInput.Value(), relX))
		return m, textinput.Blink
	case ll.headerValInputY:
		relX := msg.X - content.left
		if relX < 0 {
			relX = 0
		}
		m.headerValueInput.Focus()
		m.headerKeyInput.Blur()
		m.headerValueInput.SetCursor(displayColumnToByteOffset(m.headerValueInput.Value(), relX))
		return m, textinput.Blink
	default:
		return m, nil
	}
}

func (m Model) handleAuthEditClick(msg tea.MouseMsg) (Model, tea.Cmd) {
	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)
	content := layout.requestContentRect()

	// Auth editor: title, hint, blank, then one row per auth field.
	rowStartY := ll.editorContentY + 3
	rows := m.authEditor.rows()

	for idx, row := range rows {
		y := rowStartY + idx
		if msg.Y != y {
			continue
		}

		m.authEditor.cursor = idx
		if !authRowEditable(row) {
			return m, nil
		}

		textLeft := authRowTextLeft(content.left, row)
		relX := msg.X - textLeft
		if relX < 0 {
			relX = 0
		}

		m.authEditor.beginEdit()
		in := m.authEditor.inputForRow(row)
		in.SetCursor(displayColumnToByteOffset(in.Value(), relX))
		m.authEditor.assignInput(row, in)
		return m, textinput.Blink
	}

	return m, nil
}

func authRowEditable(row authRowID) bool {
	switch row {
	case authRowToken, authRowUsername, authRowPassword, authRowAPIKeyName, authRowAPIKeyValue:
		return true
	default:
		return false
	}
}

func authRowTextLeft(contentLeft int, row authRowID) int {
	label := "▸ " + authRowLabel(row) + ": "
	return contentLeft + lipgloss.Width(label)
}

func (e *authEditor) inputForRow(row authRowID) textinput.Model {
	switch row {
	case authRowToken:
		return e.tokenInput
	case authRowUsername:
		return e.usernameInput
	case authRowPassword:
		return e.passwordInput
	case authRowAPIKeyName:
		return e.apiKeyNameInput
	case authRowAPIKeyValue:
		return e.apiKeyValueInput
	default:
		return textinput.New()
	}
}

func (e *authEditor) assignInput(row authRowID, in textinput.Model) {
	switch row {
	case authRowToken:
		e.tokenInput = in
	case authRowUsername:
		e.usernameInput = in
	case authRowPassword:
		e.passwordInput = in
	case authRowAPIKeyName:
		e.apiKeyNameInput = in
	case authRowAPIKeyValue:
		e.apiKeyValueInput = in
	}
}

func (m Model) handleBodyTextareaClick(msg tea.MouseMsg) (Model, tea.Cmd) {
	layout := normalLayoutFor(m.width, m.height)
	ll := m.requestPaneLineLayout(layout)

	relY := msg.Y - ll.editorContentY
	relX := msg.X - m.bodyTextareaTextLeft(layout)
	if relY < 0 || relY >= m.bodyTextarea.Height() || relX < 0 {
		return m, nil
	}

	if !m.positionBodyCursorAtDisplayLine(relY, relX) {
		return m, nil
	}
	return m, nil
}

func (m *Model) positionBodyCursorAtDisplayLine(displayLine, relX int) bool {
	ta := &m.bodyTextarea
	width := ta.Width()
	if width <= 0 {
		return false
	}

	lines := strings.Split(ta.Value(), "\n")
	displayIdx := 0
	for logLine, lineText := range lines {
		wrapped := wrapRunes([]rune(lineText), width)
		if len(wrapped) > 1 {
			// Fail closed for wrapped lines — placement would be ambiguous.
			return false
		}
		if displayIdx == displayLine {
			runes := []rune(lineText)
			col := displayColumnToRuneOffset(runes, relX)
			m.navigateBodyToLogicalLine(logLine)
			ta.SetCursor(col)
			return true
		}
		displayIdx++
	}
	return false
}

func (m *Model) navigateBodyToLogicalLine(target int) {
	ta := &m.bodyTextarea
	for guard := 0; guard < 4096; guard++ {
		cur := ta.Line()
		if cur == target {
			ta.SetCursor(0)
			return
		}
		if cur < target {
			ta.CursorEnd()
			if ta.Line() >= target {
				break
			}
			ta.CursorDown()
			continue
		}
		ta.CursorStart()
		if ta.Line() > 0 {
			ta.CursorUp()
		}
	}
}
