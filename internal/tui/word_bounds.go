package tui

import (
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

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
