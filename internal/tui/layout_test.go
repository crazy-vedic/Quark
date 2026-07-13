package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDimMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    DimMode
		wantErr bool
	}{
		{"wide", DimWide, false},
		{"NARROW", DimNarrow, false},
		{" tiny ", DimTiny, false},
		{"absurd", DimAbsurd, false},
		{"", DimAuto, false},
		{"auto", DimAuto, false},
		{"huge", DimAuto, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDimMode(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDimFromSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		w, h int
		want DimMode
	}{
		{"wide roomy", 120, 40, DimWide},
		{"wide min", 80, 18, DimWide},
		{"narrow by width", 70, 40, DimNarrow},
		{"narrow by height", 120, 16, DimNarrow},
		{"narrow min", 48, 14, DimNarrow},
		{"tiny by width", 40, 40, DimTiny},
		{"tiny by height", 80, 10, DimTiny},
		{"tiny min", 24, 8, DimTiny},
		{"absurd width", 23, 40, DimAbsurd},
		{"absurd height", 80, 7, DimAbsurd},
		{"absurd both", 10, 5, DimAbsurd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, dimFromSize(tt.w, tt.h))
		})
	}
}

func TestTerminalTooSmall_MatchesAbsurd(t *testing.T) {
	t.Parallel()

	assert.True(t, terminalTooSmall(23, 8))
	assert.True(t, terminalTooSmall(24, 7))
	assert.False(t, terminalTooSmall(24, 8))
	assert.False(t, terminalTooSmall(80, 24))
}

func TestPickSidebarW_ShrinkLadder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 26, pickSidebarW(120))
	assert.Equal(t, 20, pickSidebarW(70))
	assert.Equal(t, 14, pickSidebarW(28))
	assert.Equal(t, 10, pickSidebarW(24))
}

func TestLayoutWide_PaneAt(t *testing.T) {
	t.Parallel()

	layout := layoutFor(120, 40, DimWide, sidebarPane)

	tests := []struct {
		name string
		x, y int
		want paneID
		ok   bool
	}{
		{name: "sidebar interior", x: 5, y: 5, want: sidebarPane, ok: true},
		{name: "request interior", x: 50, y: 5, want: requestPane, ok: true},
		{name: "response interior", x: 50, y: 25, want: responsePane, ok: true},
		{name: "status bar row", x: 10, y: 39, ok: false},
		{name: "outside right edge", x: 200, y: 10, ok: false},
		{name: "negative coordinates", x: -1, y: 5, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := layout.paneAt(tt.x, tt.y)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestLayoutWide_WidthInvariant(t *testing.T) {
	t.Parallel()

	for w := 40; w <= 200; w++ {
		layout := layoutFor(w, 30, DimWide, sidebarPane)
		total := layout.sidebarW + layout.mainW + paneBorderPad
		assert.LessOrEqual(t, total, w, "width=%d sidebar=%d main=%d", w, layout.sidebarW, layout.mainW)
		assert.GreaterOrEqual(t, layout.mainW, 1, "width=%d", w)
	}
}

func TestLayoutWide_SidebarPreferredAt70(t *testing.T) {
	t.Parallel()

	layout := layoutFor(70, 30, DimWide, sidebarPane)
	assert.Equal(t, DimWide, layout.mode)
	assert.Equal(t, 20, layout.sidebarW)
	assert.Equal(t, 70, layout.sidebarW+layout.mainW+paneBorderPad)
}

func TestLayoutStacked_PaneOrder(t *testing.T) {
	t.Parallel()

	layout := layoutFor(60, 30, DimNarrow, sidebarPane)
	assert.Equal(t, DimNarrow, layout.mode)

	side := layout.sidebarRect()
	req := layout.requestRect()
	resp := layout.responseRect()

	assert.Equal(t, 0, side.top)
	assert.Greater(t, req.top, side.bottom)
	assert.Greater(t, resp.top, req.bottom)
	assert.Equal(t, layout.width-1, side.right)
	assert.Equal(t, layout.width-1, req.right)

	got, ok := layout.paneAt(5, side.top+1)
	require.True(t, ok)
	assert.Equal(t, sidebarPane, got)
	got, ok = layout.paneAt(5, req.top+1)
	require.True(t, ok)
	assert.Equal(t, requestPane, got)
	got, ok = layout.paneAt(5, resp.top+1)
	require.True(t, ok)
	assert.Equal(t, responsePane, got)
}

func TestLayoutTiny_OnlyFocusedPane(t *testing.T) {
	t.Parallel()

	layout := layoutFor(40, 20, DimTiny, requestPane)
	assert.Equal(t, DimTiny, layout.mode)

	got, ok := layout.paneAt(5, 5)
	require.True(t, ok)
	assert.Equal(t, requestPane, got)

	// Sidebar/response rects are empty for non-focused panes.
	assert.False(t, layout.sidebarRect().contains(5, 5))
	assert.False(t, layout.responseRect().contains(5, 5))
}

func TestLayoutAbsurd_PaneAtFalse(t *testing.T) {
	t.Parallel()

	layout := layoutFor(10, 5, DimAbsurd, sidebarPane)
	_, ok := layout.paneAt(0, 0)
	assert.False(t, ok)
}

func TestNormalLayoutFor_AutoDim(t *testing.T) {
	t.Parallel()

	assert.Equal(t, DimWide, normalLayoutFor(120, 40).mode)
	assert.Equal(t, DimNarrow, normalLayoutFor(60, 30).mode)
	assert.Equal(t, DimTiny, normalLayoutFor(40, 20).mode)
}
