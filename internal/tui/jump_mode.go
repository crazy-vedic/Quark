package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/crazy-vedic/quark/internal/keybindings"
)

// Jump mode. The jump key labels the visible focus targets (panes, the URL,
// the body, response tabs and sidebar rows) and the next label key focuses
// its target. Esc cancels. Labels are home-row letters first; when there are
// more targets than letters, every label is two letters so that no label is
// a prefix of another.

// jumpAlphabet is the label alphabet, home row first.
const jumpAlphabet = "asdfghjklqwertyuiopzxcvbnm"

// jumpTarget is one labelled focus target. Its position is the cell where the
// label is drawn; it is computed when jump mode opens and reused for the keys
// that follow, so the labels and the keys always agree.
type jumpTarget struct {
	label string
	x, y  int
	pane  paneID
	// action runs in normal mode after pane is focused. It is empty for sidebar rows.
	action  string
	sidebar bool
	row     sidebarRow
}

// jumpState is the state of an open jump mode.
type jumpState struct {
	targets []jumpTarget
	// typed holds the first letter of a two-letter label until the second arrives.
	typed string
}

// openJumpMode labels the visible targets and enters jump mode.
func (m Model) openJumpMode() Model {
	targets := assignJumpLabels(m.jumpTargets())
	if len(targets) == 0 {
		return m.status("warn", "Nothing to jump to")
	}
	m.jump = jumpState{targets: targets}
	m.mode = jumpMode
	return m
}

// closeJumpMode leaves jump mode without acting on a target.
func (m Model) closeJumpMode() Model {
	m.mode = normalMode
	m.jump = jumpState{}
	return m
}

// handleJumpKey consumes every key while jump mode is open.
func (m Model) handleJumpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		return m.closeJumpMode(), nil
	case msg.Type == tea.KeyBackspace:
		m.jump.typed = ""
		return m, nil
	case msg.Type != tea.KeyRunes || msg.Alt || msg.Paste || len(msg.Runes) != 1:
		return m, nil
	}

	typed := m.jump.typed + strings.ToLower(string(msg.Runes))
	partial := false
	for _, t := range m.jump.targets {
		if t.label == typed {
			return m.applyJumpTarget(t)
		}
		if strings.HasPrefix(t.label, typed) {
			partial = true
		}
	}
	if partial {
		m.jump.typed = typed
	}
	// A key that starts no label is ignored; jump mode stays open.
	return m, nil
}

// applyJumpTarget leaves jump mode and does what the target's key would do.
func (m Model) applyJumpTarget(t jumpTarget) (tea.Model, tea.Cmd) {
	m = m.closeJumpMode()
	if t.sidebar {
		m.focus = sidebarPane
		m.activeField = noneField
		if t.row.kind == sidebarRequestRow {
			return m.selectSidebarRequest(t.row.colIndex, t.row.reqIndex)
		}
		return m.selectSidebarCollection(t.row.colIndex), nil
	}
	m.focus = t.pane
	return m.dispatchNormalAction(t.action)
}

// jumpTargets lists the focus targets that are on screen, in label order.
// Only panes that the current density tier draws are included.
func (m Model) jumpTargets() []jumpTarget {
	layout := m.currentLayout()
	var out []jumpTarget
	// add records a target whose label fits on screen, and reports whether it did.
	add := func(x, y int, pane paneID, action string) bool {
		if x < 0 || y < 0 || x >= m.width || y >= m.height-1 {
			return false
		}
		out = append(out, jumpTarget{x: x, y: y, pane: pane, action: action})
		return true
	}

	if r := layout.sidebarRect(); jumpRectVisible(r) {
		add(r.left+1, r.top, sidebarPane, keybindings.ActionFocusSidebar)
	}
	if jumpRectVisible(layout.requestRect()) {
		r := layout.requestRect()
		add(r.left+1, r.top, requestPane, keybindings.ActionFocusRequest)
		if url := m.requestPaneChromeRects(layout).urlLine; jumpRectVisible(url) {
			add(url.left, url.top, requestPane, keybindings.ActionEditURL)
		}
		if m.activeRequest != nil {
			if body := m.requestBodyPreviewRect(layout); jumpRectVisible(body) {
				add(body.left, body.top, requestPane, keybindings.ActionEditBody)
			}
		}
	}
	if r := layout.responseRect(); jumpRectVisible(r) {
		add(r.left+1, r.top, responsePane, keybindings.ActionFocusResponse)
		if m.selectedExecution() != nil || m.response != nil {
			tabs := m.responsePaneTabRects(layout)
			for _, t := range []struct {
				rect   layoutRect
				action string
			}{
				{tabs.body, actionTabBody},
				{tabs.headers, actionTabHeaders},
				{tabs.raw, actionTabRaw},
			} {
				if jumpRectVisible(t.rect) {
					add(t.rect.left, t.rect.top, responsePane, t.action)
				}
			}
		}
	}
	if jumpRectVisible(layout.sidebarRect()) {
		rows, start, end := m.sidebarListWindow()
		content := m.sidebarContentRect(layout)
		for idx := start; idx < end; idx++ {
			_, y, ok := m.sidebarTreeRowScreenPos(idx)
			if ok && add(content.left, y, sidebarPane, "") {
				out[len(out)-1].sidebar = true
				out[len(out)-1].row = rows[idx]
			}
		}
	}
	return out
}

// jumpRectVisible reports whether a layout rect is on screen. Panes hidden by
// the tiny tier have rects with negative coordinates.
func jumpRectVisible(r layoutRect) bool {
	return r.left >= 0 && r.top >= 0 && r.right >= r.left && r.bottom >= r.top
}

// assignJumpLabels gives each target its label. It keeps at most the number of
// two-letter labels the alphabet allows.
func assignJumpLabels(targets []jumpTarget) []jumpTarget {
	n := len(jumpAlphabet)
	if len(targets) > n*n {
		targets = targets[:n*n]
	}
	for i := range targets {
		if len(targets) <= n {
			targets[i].label = jumpAlphabet[i : i+1]
			continue
		}
		targets[i].label = jumpAlphabet[i/n:i/n+1] + jumpAlphabet[i%n:i%n+1]
	}
	return targets
}

// viewJumpMode draws the normal frame with each label over its target and a
// prompt in the status row.
func (m Model) viewJumpMode() string {
	lines := strings.Split(m.viewByDim(), "\n")
	style := jumpLabelStyle(m.styles)
	for _, t := range m.jump.targets {
		if t.y < 0 || t.y >= len(lines) {
			continue
		}
		lines[t.y] = spliceJumpLabel(lines[t.y], t.x, style.Render(t.label))
	}
	if n := len(lines); n > 0 {
		prompt := "jump"
		if m.jump.typed != "" {
			prompt += " " + m.jump.typed
		}
		prompt += " - type a label, esc cancels"
		lines[n-1] = m.styles.mutedStyle.Render(truncate(prompt, max(1, m.width)))
	}
	return strings.Join(lines, "\n")
}

func jumpLabelStyle(s styleSet) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(s.accent).Reverse(true)
}

// spliceJumpLabel replaces the cells that label covers at x in line, keeping
// the ANSI styling on both sides.
func spliceJumpLabel(line string, x int, label string) string {
	if x < 0 {
		return line
	}
	left := ansi.Truncate(line, x, "")
	if pad := x - ansi.StringWidth(left); pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	right := ansi.TruncateLeft(line, x+ansi.StringWidth(label), "")
	return left + label + right
}
