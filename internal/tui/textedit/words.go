package textedit

import "unicode"

// IsWordChar reports whether r is part of a word: letters, digits, underscore.
// Hyphens separate words.
func IsWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

// WordBoundsAt returns the [start, end) range of the word at pos. For a
// non-word character it returns that character alone.
func WordBoundsAt(runes []rune, pos int) (start, end int) {
	if len(runes) == 0 {
		return 0, 0
	}
	pos = clamp(pos, len(runes))
	if pos == len(runes) && pos > 0 {
		pos--
	}
	if pos >= len(runes) {
		return pos, pos
	}
	if !IsWordChar(runes[pos]) {
		return pos, pos + 1
	}
	start = pos
	for start > 0 && IsWordChar(runes[start-1]) {
		start--
	}
	end = pos + 1
	for end < len(runes) && IsWordChar(runes[end]) {
		end++
	}
	return start, end
}

// NextWordBoundary returns the position after skipping the word at pos and
// the separators following it, or len(runes) at the end.
func NextWordBoundary(runes []rune, pos int) int {
	if pos < 0 || len(runes) == 0 {
		return 0
	}
	if pos >= len(runes) {
		return len(runes)
	}
	if IsWordChar(runes[pos]) {
		for pos < len(runes) && IsWordChar(runes[pos]) {
			pos++
		}
	}
	for pos < len(runes) && !IsWordChar(runes[pos]) {
		pos++
	}
	return pos
}

// PrevWordBoundary returns the start of the word before pos, or 0.
func PrevWordBoundary(runes []rune, pos int) int {
	if pos <= 0 || len(runes) == 0 {
		return 0
	}
	pos = clamp(pos, len(runes))
	pos--
	for pos > 0 && !IsWordChar(runes[pos]) {
		pos--
	}
	for pos > 0 && IsWordChar(runes[pos-1]) {
		pos--
	}
	return pos
}

func clamp(v, hi int) int {
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}
