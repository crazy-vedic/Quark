package highlight

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSON_ValidJSON(t *testing.T) {
	input := `{"name":"test","value":42,"active":true}`
	result := JSON(input, "dark")

	// Should contain ANSI escape codes (color codes) for a highlighted output.
	require.NotEmpty(t, result)
	assert.Contains(t, result, "name")
	assert.Contains(t, result, "test")
	assert.Contains(t, result, "\x1b[") // ANSI escape sequence present

	// Should NOT contain dangerous OSC sequences (clipboard hijack, etc.)
	assert.NotContains(t, result, "\x1b]")
}

func TestJSON_PrettyPrinted(t *testing.T) {
	input := "{\n  \"key\": \"value\"\n}"
	result := JSON(input, "dark")

	require.NotEmpty(t, result)
	assert.Contains(t, result, "key")
	assert.Contains(t, result, "value")
}

func TestJSON_InvalidJSON_ColorizesPlainText(t *testing.T) {
	// Chroma tokenizes plain text and produces colorized output
	input := "not json at all"
	result := JSON(input, "dark")

	// Should still return the original content (possibly with ANSI color codes between chars)
	require.NotEmpty(t, result)
	// Each character should be present, even if wrapped in ANSI codes
	for _, ch := range input {
		assert.Contains(t, result, string(ch), "character %q should be in output", ch)
	}
	// Should contain ANSI escape sequences (colorized output)
	assert.Contains(t, result, "\x1b[")
}

func TestJSON_EmptyString(t *testing.T) {
	result := JSON("", "dark")
	assert.Empty(t, result)
}

func TestJSON_ThemeMapping(t *testing.T) {
	input := `{"test": 1}`

	dark := JSON(input, "dark")
	light := JSON(input, "light")
	auto := JSON(input, "auto")
	unknown := JSON(input, "nonexistent")

	// All should produce non-empty output
	require.NotEmpty(t, dark)
	require.NotEmpty(t, light)
	require.NotEmpty(t, auto)
	require.NotEmpty(t, unknown)

	// Different themes may produce different ANSI codes
	// but all should contain the original content
	assert.Contains(t, dark, "test")
	assert.Contains(t, light, "test")
	assert.Contains(t, auto, "test")
	assert.Contains(t, unknown, "test")
}

func TestSafeStrip_RemovesOSCHijack(t *testing.T) {
	// OSC 52 clipboard hijack sequence
	input := "\x1b]52;c;ZWNobyB0ZXN0\x07"
	result := safeStrip(input)
	assert.Empty(t, result)
}

func TestSafeStrip_RemovesCursorMovement(t *testing.T) {
	input := "\x1b[2J\x1b[H" // clear screen + home cursor
	result := safeStrip(input)
	assert.Empty(t, result)
}

func TestSafeStrip_PreservesColorCodes(t *testing.T) {
	input := "\x1b[38;5;123mhello\x1b[0m"
	result := safeStrip(input)
	assert.Contains(t, result, "hello")
	assert.Contains(t, result, "\x1b[")
}

func TestSafeStrip_MixedContent(t *testing.T) {
	input := "\x1b[38;5;123mhello\x1b[0m\x1b]52;c;test\x07\x1b[2Jworld"
	result := safeStrip(input)
	assert.Contains(t, result, "hello")
	assert.Contains(t, result, "world")
	assert.NotContains(t, result, "\x1b]")
	assert.NotContains(t, result, "\x1b[2J")
}

func TestJSON_LargeJSON(t *testing.T) {
	// Build a large JSON structure
	var sb strings.Builder
	sb.WriteString("[\n")
	for i := 0; i < 100; i++ {
		if i > 0 {
			sb.WriteString(",\n")
		}
		sb.WriteString(`  {"id": `)
		sb.WriteString(string(rune('0' + (i % 10))))
		sb.WriteString(`, "name": "item`)
		sb.WriteString(string(rune('0' + (i % 10))))
		sb.WriteString(`"}`)
	}
	sb.WriteString("\n]")

	result := JSON(sb.String(), "dark")
	require.NotEmpty(t, result)
	assert.Contains(t, result, "item0")
	assert.Contains(t, result, "id")
}
