package tui

import (
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// isWordChar returns true if r is part of a word. Words consist of letters,
// digits, and underscores. Hyphens are treated as word separators to allow
// proper word selection in hyphenated identifiers (e.g., content-type -> select
// "content" or "type" separately, not the whole hyphenated word).
func isWordChar(r rune) bool { return textedit.IsWordChar(r) }

// wordBoundsAt finds the start and end (in rune offsets) of the word at or
// containing the given rune position. If pos is at whitespace or punctuation,
// finds the bounds of that character only. Returns [start, end) range.
func wordBoundsAt(runes []rune, pos int) (start, end int) {
	return textedit.WordBoundsAt(runes, pos)
}

// stringToRunes converts a string to a slice of runes, handling multi-byte
// UTF-8 sequences correctly. This is needed because terminals think in terms
// of display columns and runes, but Go strings are byte sequences.
func stringToRunes(s string) []rune {
	return []rune(s)
}

// nextWordBoundary finds the position of the next word boundary when moving
// forward from pos. A word boundary is where the word-character status changes.
// Returns the position of the next boundary, or len(runes) if at the end.
func nextWordBoundary(runes []rune, pos int) int { return textedit.NextWordBoundary(runes, pos) }

// prevWordBoundary finds the position of the previous word boundary when
// moving backward from pos. Returns 0 if at the start.
func prevWordBoundary(runes []rune, pos int) int { return textedit.PrevWordBoundary(runes, pos) }
