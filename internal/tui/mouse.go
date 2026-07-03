package tui

import (
	tea "github.com/charmbracelet/bubbletea"

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

	layout := normalLayoutFor(m.width, m.height)

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
		m.focus = responsePane
		m.activeField = noneField
	}
	return m, nil
}

func (m Model) handleRequestClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := normalLayoutFor(m.width, m.height)
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
			return m.handleBodyTextareaClick(msg)
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
	layout := normalLayoutFor(m.width, m.height)
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
		return m.selectSidebarCollection(hit.row.colIndex)
	case sidebarListHitCollectionDisclosure:
		return m.toggleSidebarCollectionExpand(hit.row.colIndex)
	case sidebarListHitRequest:
		return m.selectSidebarRequest(hit.row.colIndex, hit.row.reqIndex)
	default:
		return m, nil
	}
}

func (m Model) selectSidebarCollection(colIndex int) (Model, tea.Cmd) {
	if colIndex < 0 || colIndex >= len(m.collections) {
		return m, nil
	}
	m.colCursor = colIndex
	m.reqCursor = -1
	m = m.ensureSidebarCollectionVisible()
	if colID := m.collections[colIndex].ID; m.expanded[colID] {
		m.requests = m.collectionRequests[colID]
	}
	return m, nil
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

	layout := normalLayoutFor(m.width, m.height)
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

	layout := normalLayoutFor(m.width, m.height)
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
