package tui

import (
	"strings"
	"unicode"

	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// displayColumnToByteOffset maps a terminal display column within a string to a
// byte offset suitable for bubbles textinput SetCursor. Clicks past the end
// clamp to len(text).
func displayColumnToByteOffset(text string, col int) int {
	if col <= 0 || text == "" {
		return 0
	}

	disp := 0
	byteIdx := 0
	for _, r := range text {
		rw := rw.RuneWidth(r)
		if disp >= col {
			break
		}
		if disp+rw > col {
			break
		}
		disp += rw
		byteIdx += len(string(r))
	}
	return min(byteIdx, len(text))
}

// displayColumnToRuneOffset maps a terminal display column within a rune slice
// to a rune index suitable for bubbles textarea SetCursor.
func displayColumnToRuneOffset(runes []rune, col int) int {
	if col <= 0 || len(runes) == 0 {
		return 0
	}

	disp := 0
	for i, r := range runes {
		rw := rw.RuneWidth(r)
		if disp >= col {
			return i
		}
		if disp+rw > col {
			return i
		}
		disp += rw
	}
	return len(runes)
}

// wrapRunes soft-wraps runes to the given display width using the same rules
// as charmbracelet/bubbles textarea wrap().
func wrapRunes(runes []rune, width int) [][]rune {
	if width <= 0 {
		return [][]rune{runes}
	}

	var (
		lines  = [][]rune{{}}
		word   []rune
		row    int
		spaces int
	)

	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}

		if spaces > 0 {
			if uniseg.StringWidth(
				string(lines[row]),
			)+uniseg.StringWidth(
				string(word),
			)+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpacesRunes(spaces)...)
				spaces = 0
				word = nil
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], repeatSpacesRunes(spaces)...)
				spaces = 0
				word = nil
			}
		} else if len(word) > 0 {
			lastCharLen := rw.RuneWidth(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}

	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		spaces++
		lines[row+1] = append(lines[row+1], repeatSpacesRunes(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], repeatSpacesRunes(spaces)...)
	}

	return lines
}

func repeatSpacesRunes(n int) []rune {
	return []rune(strings.Repeat(" ", n))
}
