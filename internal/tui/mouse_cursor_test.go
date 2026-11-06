package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisplayColumnToByteOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		col  int
		want int
	}{
		{name: "empty", text: "", col: 5, want: 0},
		{name: "start", text: "hello", col: 0, want: 0},
		{name: "middle", text: "https://example.test/path", col: 8, want: 8},
		{name: "end", text: "abc", col: 99, want: 3},
		{name: "wide", text: "a世b", col: 2, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, displayColumnToByteOffset(tt.text, tt.col))
		})
	}
}

func TestDisplayColumnToRuneOffset(t *testing.T) {
	t.Parallel()

	runes := []rune("line")
	assert.Equal(t, 2, displayColumnToRuneOffset(runes, 2))
	assert.Equal(t, 4, displayColumnToRuneOffset(runes, 99))
}

func TestWrapRunes_NoWrapWhenFits(t *testing.T) {
	t.Parallel()

	got := wrapRunes([]rune("short"), 40)
	assert.Len(t, got, 1)
	assert.Equal(t, "short ", string(got[0]))
}

func TestWrapRunes_WrapsLongLine(t *testing.T) {
	t.Parallel()

	got := wrapRunes([]rune("abcdefghij"), 4)
	assert.Greater(t, len(got), 1)
}
