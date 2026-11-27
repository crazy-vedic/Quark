package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// softWindowSizeMsg updates model size without going through tea.WindowSizeMsg.
// Bubble Tea's renderer calls repaint() (clears the line cache) on every
// WindowSizeMsg; for pure width shrinks that full rewrite is what still
// flickers. Soft shrinks keep lastRenderedLines so unchanged rows (sidebar)
// can be skipped on the next flush.
type softWindowSizeMsg struct {
	Width  int
	Height int
}

// ResizeCoalescer filters resize traffic before Bubble Tea handles it.
type ResizeCoalescer struct{}

// NewResizeCoalescer returns a filter that drops duplicate window sizes and
// soft-applies pure width shrinks when density mode is unchanged.
func NewResizeCoalescer() *ResizeCoalescer {
	return &ResizeCoalescer{}
}

// Bind is a no-op kept for call-site compatibility with tea.NewProgram wiring.
func (c *ResizeCoalescer) Bind(_ func(tea.Msg)) {}

// Filter implements tea.WithFilter.
func (c *ResizeCoalescer) Filter(m tea.Model, msg tea.Msg) tea.Msg {
	ws, ok := msg.(tea.WindowSizeMsg)
	if !ok {
		return msg
	}
	tm, ok := m.(Model)
	if !ok {
		return msg
	}
	if tm.width == 0 || tm.height == 0 {
		return ws
	}
	if ws.Width == tm.width && ws.Height == tm.height {
		return nil
	}

	prevDim := tm.effectiveDim()
	nextDim := dimWithHysteresis(ws.Width, ws.Height, tm.stickyDim)
	dimChanging := nextDim != prevDim

	// Height changes, width growth, or density-tier changes must go through
	// WindowSizeMsg so the renderer clears its line cache (layout morphs).
	if ws.Height != tm.height || ws.Width > tm.width || dimChanging {
		return ws
	}

	// Pure width shrink within the same density tier: soft path.
	return softWindowSizeMsg{Width: ws.Width, Height: ws.Height}
}
