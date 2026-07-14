package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResizeCoalescer_DropsDuplicateSize(t *testing.T) {
	m := Model{width: 120, height: 40, stickyDim: DimWide}
	c := NewResizeCoalescer()
	assert.Nil(t, c.Filter(m, tea.WindowSizeMsg{Width: 120, Height: 40}))
}

func TestResizeCoalescer_PassesInitialSize(t *testing.T) {
	m := Model{}
	c := NewResizeCoalescer()
	msg := tea.WindowSizeMsg{Width: 100, Height: 30}
	assert.Equal(t, msg, c.Filter(m, msg))
}

func TestResizeCoalescer_SoftShrinkSameDim(t *testing.T) {
	m := Model{width: 120, height: 40, stickyDim: DimWide}
	c := NewResizeCoalescer()
	out := c.Filter(m, tea.WindowSizeMsg{Width: 110, Height: 40})
	soft, ok := out.(softWindowSizeMsg)
	require.True(t, ok, "same-dim width shrink must be soft")
	assert.Equal(t, 110, soft.Width)
}

func TestResizeCoalescer_HardWhenDimWouldChange(t *testing.T) {
	// sticky wide; shrink past exit band (75) → narrow → must hard-repaint
	m := Model{width: 80, height: 40, stickyDim: DimWide}
	c := NewResizeCoalescer()
	out := c.Filter(m, tea.WindowSizeMsg{Width: 75, Height: 40})
	_, isSoft := out.(softWindowSizeMsg)
	assert.False(t, isSoft)
	assert.Equal(t, tea.WindowSizeMsg{Width: 75, Height: 40}, out)
}

func TestResizeCoalescer_HardOnGrow(t *testing.T) {
	m := Model{width: 100, height: 40, stickyDim: DimWide}
	c := NewResizeCoalescer()
	msg := tea.WindowSizeMsg{Width: 140, Height: 40}
	assert.Equal(t, msg, c.Filter(m, msg))
}

func TestUpdate_ApplyDimStickyOnResize(t *testing.T) {
	m := Model{width: 120, height: 40, stickyDim: DimWide}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 79, Height: 40})
	got := next.(Model)
	assert.Equal(t, 79, got.width)
	assert.Equal(t, DimWide, got.stickyDim, "79 is inside hysteresis; stay wide")
	assert.Equal(t, DimWide, got.effectiveDim())

	next, _ = got.Update(tea.WindowSizeMsg{Width: 75, Height: 40})
	got = next.(Model)
	assert.Equal(t, DimNarrow, got.stickyDim)
	assert.Equal(t, DimNarrow, got.effectiveDim())
}
