package tui

import "unicode"

// isWordChar returns true if r is part of a word. Words consist of letters,
// digits, and underscores. Hyphens are treated as word separators to allow
// proper word selection in hyphenated identifiers (e.g., content-type -> select
// "content" or "type" separately, not the whole hyphenated word).
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

// wordBoundsAt finds the start and end (in rune offsets) of the word at or
// containing the given rune position. If pos is at whitespace or punctuation,
// finds the bounds of that character only. Returns [start, end) range.
func wordBoundsAt(runes []rune, pos int) (start, end int) {
	if len(runes) == 0 {
		return 0, 0
	}
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	// If we're at the end of the string, back up one position to find the word.
	if pos == len(runes) && pos > 0 {
		pos--
	}

	// Determine if the character at pos is a word character.
	if pos >= len(runes) {
		return pos, pos
	}

	r := runes[pos]
	if !isWordChar(r) {
		// Non-word character: select just itself.
		return pos, pos + 1
	}

	// Word character: find the start and end of the word.
	start = pos
	for start > 0 && isWordChar(runes[start-1]) {
		start--
	}

	end = pos + 1
	for end < len(runes) && isWordChar(runes[end]) {
		end++
	}

	return start, end
}

// stringToRunes converts a string to a slice of runes, handling multi-byte
// UTF-8 sequences correctly. This is needed because terminals think in terms
// of display columns and runes, but Go strings are byte sequences.
func stringToRunes(s string) []rune {
	return []rune(s)
}

// runesToString converts a rune slice back to a string.
func runesToString(runes []rune) string {
	return string(runes)
}

// byteOffsetToRuneOffset converts a byte offset in a string to a rune offset.
// This is needed because bubbles textinput uses byte offsets, but selection
// logic works in rune offsets.
func byteOffsetToRuneOffset(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff >= len(s) {
		return len(stringToRunes(s))
	}
	return len(stringToRunes(s[:byteOff]))
}

// runeOffsetToByteOffset converts a rune offset back to a byte offset.
func runeOffsetToByteOffset(s string, runeOff int) int {
	runes := stringToRunes(s)
	if runeOff <= 0 {
		return 0
	}
	if runeOff >= len(runes) {
		return len(s)
	}
	return len(runesToString(runes[:runeOff]))
}

// selectedText extracts the selected substring from a string given a rune-based
// selection (start, end). Handles out-of-bounds safely.
func selectedText(s string, start, end int) string {
	runes := stringToRunes(s)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return ""
	}
	return runesToString(runes[start:end])
}

// removeSelectedRange removes the selected substring from s and returns the
// modified string and the new cursor position (at the start of the deletion).
func removeSelectedRange(s string, start, end int) (string, int) {
	runes := stringToRunes(s)
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return s, start
	}
	result := make([]rune, 0, len(runes)-len(runes[start:end]))
	result = append(result, runes[:start]...)
	result = append(result, runes[end:]...)
	return runesToString(result), start
}

// nextWordBoundary finds the position of the next word boundary when moving
// forward from pos. A word boundary is where the word-character status changes.
// Returns the position of the next boundary, or len(runes) if at the end.
func nextWordBoundary(runes []rune, pos int) int {
	if pos < 0 || len(runes) == 0 {
		return 0
	}
	if pos >= len(runes) {
		return len(runes)
	}

	// If we're in a word, skip to the end of it.
	if isWordChar(runes[pos]) {
		for pos < len(runes) && isWordChar(runes[pos]) {
			pos++
		}
	}

	// Skip any non-word characters (whitespace, punctuation).
	for pos < len(runes) && !isWordChar(runes[pos]) {
		pos++
	}

	return pos
}

// prevWordBoundary finds the position of the previous word boundary when
// moving backward from pos. Returns 0 if at the start.
func prevWordBoundary(runes []rune, pos int) int {
	if pos <= 0 || len(runes) == 0 {
		return 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	pos--

	// Skip any non-word characters.
	for pos > 0 && !isWordChar(runes[pos]) {
		pos--
	}

	// If we're in a word, skip to the start of it.
	for pos > 0 && isWordChar(runes[pos-1]) {
		pos--
	}

	return pos
}

// getTextareaAbsoluteCursorPos converts a textarea's line-based cursor position
// to an absolute rune offset into the full text. This is needed for selection
// which works with rune offsets, not line/column pairs.
// Note: Since textarea doesn't expose col publicly, this uses a simplified approach
// that counts all runes up to and including the current line.
func getTextareaAbsoluteCursorPos(text string, line int) int {
	if line < 0 {
		return 0
	}

	runes := stringToRunes(text)
	lineIdx := 0
	pos := 0

	for i, r := range runes {
		if lineIdx == line {
			// Found target line, return the position at the start of this line
			return pos
		}
		if r == '\n' {
			lineIdx++
			pos = i + 1
		}
	}

	// Line not found or at end, return end of text
	return len(runes)
}

// setTextareaCursorFromAbsolutePos converts an absolute rune offset back to
// line/column pair for textarea SetCursor. For now, we just set the column
// offset relative to the current line, which is a simplification.
// A proper implementation would need to navigate to the correct line first.
func setTextareaCursorFromAbsolutePos(text string, absPos int) (line int, col int) {
	if absPos < 0 {
		return 0, 0
	}

	runes := stringToRunes(text)
	if absPos >= len(runes) {
		absPos = len(runes)
	}

	currentLine := 0
	lineStart := 0

	for i := 0; i < absPos && i < len(runes); i++ {
		if runes[i] == '\n' {
			currentLine++
			lineStart = i + 1
		}
	}

	return currentLine, absPos - lineStart
}
