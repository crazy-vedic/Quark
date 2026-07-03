package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalLayout_PaneAt(t *testing.T) {
	t.Parallel()

	layout := normalLayoutFor(120, 40)

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

func TestNormalLayout_PaneAtZeroSize(t *testing.T) {
	t.Parallel()

	layout := normalLayoutFor(0, 0)
	_, ok := layout.paneAt(0, 0)
	assert.False(t, ok)
}

func TestNormalLayout_NarrowWidth(t *testing.T) {
	t.Parallel()

	layout := normalLayoutFor(70, 30)
	got, ok := layout.paneAt(5, 5)
	assert.True(t, ok)
	assert.Equal(t, sidebarPane, got)
	assert.Equal(t, 20, layout.sidebarW)
}
