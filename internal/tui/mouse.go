package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/keybindings"
)

const (
	sidebarDisclosureStartX = 2
	sidebarDisclosureEndX   = 3
)

type sidebarListHitKind int

const (
	sidebarListHitTitle sidebarListHitKind = iota
	sidebarListHitMoreAbove
	sidebarListHitMoreBelow
	sidebarListHitCollection
	sidebarListHitCollectionDisclosure
	sidebarListHitRequest
)

type sidebarListHit struct {
	kind     sidebarListHitKind
	row      sidebarRow
	rowIndex int
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != normalMode {
		return m, nil
	}
	if m.effectiveDim() == DimAbsurd {
		return m, nil
	}

	layout := m.currentLayout()

	if isMouseWheel(msg) {
		pane, ok := layout.paneAt(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		switch pane {
		case sidebarPane:
			return m.handleSidebarWheel(msg)
		case requestPane:
			if m.activeField == bodyField {
				ll := m.requestPaneLineLayout(layout)
				if msg.Y >= ll.editorContentY && msg.Y < ll.editorContentY+m.bodyTextarea.Height() {
					var cmd tea.Cmd
					m.bodyTextarea, cmd = m.bodyTextarea.Update(msg)
					return m, cmd
				}
			}
		case responsePane:
			return m.handleResponseWheel(msg)
		}
		return m, nil
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	pane, ok := layout.paneAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}

	switch pane {
	case sidebarPane:
		return m.handleSidebarClick(msg)
	case requestPane:
		return m.handleRequestClick(msg)
	case responsePane:
		return m.handleResponseClick(msg)
	}
	return m, nil
}

func (m Model) handleResponseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := m.currentLayout()
	m.focus = responsePane
	m.activeField = noneField

	if m.selectedExecution() == nil && m.response == nil {
		return m, nil
	}

	tabs := m.responsePaneTabRects(layout)
	switch {
	case tabs.body.contains(msg.X, msg.Y):
		return m.handleResponseAction("tab_body")
	case tabs.headers.contains(msg.X, msg.Y):
		return m.handleResponseAction("tab_headers")
	case tabs.raw.contains(msg.X, msg.Y):
		return m.handleResponseAction("tab_raw")
	}

	if idx, ok := m.responseHistoryHitAt(msg.X, msg.Y, layout); ok {
		m.execCursor = idx
		return m, nil
	}
	return m, nil
}

func (m Model) handleResponseWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.focus = responsePane
	m.activeField = noneField
	if len(m.executions) <= 1 {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		return m.handleResponseAction("history_next")
	case tea.MouseButtonWheelUp:
		return m.handleResponseAction("history_prev")
	default:
		return m, nil
	}
}

// responseHistoryHitAt returns the execution index under (x,y) when the history
// popup is visible on the right side of the response pane.
func (m Model) responseHistoryHitAt(x, y int, layout normalLayout) (int, bool) {
	if !m.viewingHistoricalExecution() {
		return 0, false
	}
	content := layout.responseContentRect()
	innerW := max(1, content.right-content.left+1)
	if innerW < 64 {
		return 0, false
	}

	tabs := m.responsePaneTabRects(layout)
	// Body region starts two rows after the tab bar (tab line + blank line).
	bodyTop := tabs.tabBarY + 2
	bodyBottom := content.bottom
	bodyLines := bodyBottom - bodyTop + 1
	if bodyLines < 6 {
		return 0, false
	}

	popup := m.viewExecutionHistoryPopup(innerW, bodyLines)
	if popup == "" {
		return 0, false
	}
	popupW := lipgloss.Width(popup)
	bodyWidth := innerW - popupW - 4
	if bodyWidth < 18 {
		return 0, false
	}

	popupLeft := content.left + bodyWidth + 2 // gap between body and popup
	popupRight := popupLeft + popupW - 1
	if x < popupLeft || x > popupRight || y < bodyTop {
		return 0, false
	}

	maxVisible := min(historyPopupVisibleRows, bodyLines-3)
	if maxVisible < 3 {
		return 0, false
	}
	indices, _, _ := m.visibleExecutionHistoryWindow(maxVisible)
	// Popup chrome: title row, then "more above"/blank row, then history lines.
	// Border adds 1 row/col of padding around the inner content.
	innerY := y - bodyTop - 1 // account for top border
	row := innerY - 2         // skip title + spacer
	if row < 0 || row >= len(indices) {
		return 0, false
	}
	return indices[row], true
}

func (m Model) handleRequestClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := m.currentLayout()
	chrome := m.requestPaneChromeRects(layout)

	m.focus = requestPane

	switch {
	case chrome.sendButton.contains(msg.X, msg.Y):
		return m.handleRequestAction(keybindings.ActionSendRequest)
	case chrome.methodBadge.contains(msg.X, msg.Y):
		return m.handleRequestAction(keybindings.ActionMethodNext)
	case chrome.urlLine.contains(msg.X, msg.Y):
		return m.handleURLLineClick(msg)
	}

	switch m.activeField {
	case headersField:
		if m.headerEditing {
			return m.handleHeaderEditClick(msg)
		}
		return m.handleHeaderListClick(msg)
	case authField:
		return m.handleAuthEditClick(msg)
	case bodyField:
		ll := m.requestPaneLineLayout(layout)
		if msg.Y >= ll.editorContentY && msg.Y < ll.editorContentY+m.bodyTextarea.Height() {
			return m.handleBodyTextareaClick(msg), nil
		}
	}

	return m, nil
}

func (m Model) handleSidebarWheel(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelDown:
		return m.sidebarDown()
	case tea.MouseButtonWheelUp:
		return m.sidebarUp()
	default:
		return m, nil
	}
}

func (m Model) handleSidebarClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := m.currentLayout()
	if !layout.sidebarRect().contains(msg.X, msg.Y) {
		return m, nil
	}

	m.focus = sidebarPane
	m.activeField = noneField

	hit, ok := m.sidebarListHitAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}

	switch hit.kind {
	case sidebarListHitTitle:
		return m, nil
	case sidebarListHitMoreAbove:
		if m.sidebarOffset > 0 {
			m.sidebarOffset--
		}
		return m, nil
	case sidebarListHitMoreBelow:
		rows, _ := m.buildSidebarRows()
		maxScroll := max(0, len(rows)-m.sidebarVisible())
		if m.sidebarOffset < maxScroll {
			m.sidebarOffset++
		}
		return m, nil
	case sidebarListHitCollection:
		if m.colCursor == hit.row.colIndex && m.reqCursor == -1 {
			return m.toggleSidebarCollectionExpand(hit.row.colIndex)
		}
		return m.selectSidebarCollection(hit.row.colIndex), nil
	case sidebarListHitCollectionDisclosure:
		return m.toggleSidebarCollectionExpand(hit.row.colIndex)
	case sidebarListHitRequest:
		return m.selectSidebarRequest(hit.row.colIndex, hit.row.reqIndex)
	default:
		return m, nil
	}
}

func (m Model) selectSidebarCollection(colIndex int) Model {
	if colIndex < 0 || colIndex >= len(m.collections) {
		return m
	}
	m.colCursor = colIndex
	m.reqCursor = -1
	m = m.ensureSidebarCollectionVisible()
	if colID := m.collections[colIndex].ID; m.expanded[colID] {
		m.requests = m.collectionRequests[colID]
	}
	return m
}

func (m Model) toggleSidebarCollectionExpand(colIndex int) (Model, tea.Cmd) {
	if colIndex < 0 || colIndex >= len(m.collections) {
		return m, nil
	}
	m.colCursor = colIndex
	m.reqCursor = -1
	m = m.ensureSidebarCollectionVisible()

	colID := m.collections[colIndex].ID
	if m.expanded[colID] {
		m.expanded[colID] = false
		delete(m.collectionRequests, colID)
		return m, nil
	}

	m.expanded[colID] = true
	if m.reader != nil {
		return m, loadRequestsCmd(m.ctx, m.reader, colID)
	}
	return m, nil
}

func (m Model) selectSidebarRequest(colIndex, reqIndex int) (Model, tea.Cmd) {
	if colIndex < 0 || colIndex >= len(m.collections) {
		return m, nil
	}
	colID := m.collections[colIndex].ID
	reqs := m.collectionRequests[colID]
	if reqIndex < 0 || reqIndex >= len(reqs) {
		return m, nil
	}

	m.colCursor = colIndex
	m.reqCursor = reqIndex
	m = m.ensureSidebarCollectionVisible()
	m.requests = reqs
	m.focus = requestPane
	return m.selectRequest(reqs[reqIndex])
}

func (m Model) sidebarContentRect(layout normalLayout) layoutRect {
	r := layout.sidebarRect()
	return layoutRect{
		left:   r.left + 1,
		top:    r.top + 1,
		right:  r.right - 1,
		bottom: r.bottom - 1,
	}
}

func (m Model) sidebarListWindow() (rows []sidebarRow, start, end int) {
	rows, _ = m.buildSidebarRows()
	visible := m.sidebarVisible()
	start = min(m.sidebarOffset, max(0, len(rows)-visible))
	end = min(len(rows), start+visible)
	return rows, start, end
}

func (m Model) sidebarListHitAt(x, y int) (sidebarListHit, bool) {
	if m.width <= 0 || m.height <= 0 {
		return sidebarListHit{}, false
	}

	layout := m.currentLayout()
	content := m.sidebarContentRect(layout)
	if !content.contains(x, y) {
		return sidebarListHit{}, false
	}

	innerY := y - content.top
	if innerY == 0 {
		return sidebarListHit{kind: sidebarListHitTitle}, true
	}

	rows, start, end := m.sidebarListWindow()
	listLine := innerY - 1
	line := 0

	if start > 0 {
		if listLine == line {
			return sidebarListHit{kind: sidebarListHitMoreAbove}, true
		}
		line++
	}

	visibleRows := end - start
	if listLine-line < visibleRows {
		idx := start + (listLine - line)
		row := rows[idx]
		contentX := x - content.left
		kind := sidebarListHitCollection
		switch row.kind {
		case sidebarRequestRow:
			kind = sidebarListHitRequest
		case sidebarCollectionRow:
			if contentX >= sidebarDisclosureStartX && contentX <= sidebarDisclosureEndX {
				kind = sidebarListHitCollectionDisclosure
			}
		}
		return sidebarListHit{kind: kind, row: row, rowIndex: idx}, true
	}
	line += visibleRows

	if end < len(rows) && listLine == line {
		return sidebarListHit{kind: sidebarListHitMoreBelow}, true
	}

	return sidebarListHit{}, false
}

func (m Model) sidebarTreeRowScreenPos(rowIndex int) (x, y int, ok bool) {
	rows, start, end := m.sidebarListWindow()
	if rowIndex < start || rowIndex >= end {
		return 0, 0, false
	}

	layout := m.currentLayout()
	content := m.sidebarContentRect(layout)

	listLine := rowIndex - start
	if start > 0 {
		listLine++
	}
	y = content.top + 1 + listLine
	x = content.left + 5
	if rowIndex < len(rows) && rows[rowIndex].kind == sidebarRequestRow {
		x = content.left + 6
	}
	return x, y, true
}

func (m Model) sidebarRowIndexForCollection(colIndex int) (int, bool) {
	rows, _ := m.buildSidebarRows()
	for i, row := range rows {
		if row.kind == sidebarCollectionRow && row.colIndex == colIndex {
			return i, true
		}
	}
	return 0, false
}

func (m Model) sidebarRowIndexForRequest(colIndex, reqIndex int) (int, bool) {
	rows, _ := m.buildSidebarRows()
	for i, row := range rows {
		if row.kind == sidebarRequestRow && row.colIndex == colIndex && row.reqIndex == reqIndex {
			return i, true
		}
	}
	return 0, false
}

func isMouseWheel(msg tea.MouseMsg) bool {
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown,
		tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		return true
	default:
		return false
	}
}
