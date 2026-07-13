package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFrameOverflows_HeightAndWidth(t *testing.T) {
	t.Parallel()

	tall := strings.Repeat("line\n", 20)
	assert.True(t, frameOverflows(tall, 80, 5))
	assert.False(t, frameOverflows("ok", 80, 24))

	wide := strings.Repeat("w", 100)
	assert.True(t, frameOverflows(wide, 40, 24))
	assert.False(t, frameOverflows("narrow", 40, 24))
}
