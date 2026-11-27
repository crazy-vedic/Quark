//go:build e2e

package tui_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// screenCell is the terminal's effective color at one displayed cell, after
// nested ANSI styles and resets. Empty colors mean the terminal default.
type screenCell struct {
	char    rune
	fg      string
	bg      string
	reverse bool
	seen    bool
}

type themeScreen struct {
	rows  [][]screenCell
	width int
}

// renderThemeScreen interprets the SGR subset emitted by Lip Gloss and
// Chroma. This tests the final frame, where a style reset in one pane can
// otherwise expose the terminal's own background in only part of the UI.
func renderThemeScreen(t *testing.T, frame string, width, height int) themeScreen {
	t.Helper()
	s := themeScreen{rows: make([][]screenCell, height), width: width}
	for y := range s.rows {
		s.rows[y] = make([]screenCell, width)
	}
	x, y := 0, 0
	fg, bg, reverse := "", "", false
	for i := 0; i < len(frame); {
		if frame[i] == '\x1b' {
			if i+1 >= len(frame) || frame[i+1] != '[' {
				t.Fatalf("unsupported terminal escape at byte %d", i)
			}
			end := strings.IndexByte(frame[i+2:], 'm')
			if end < 0 {
				t.Fatalf("unterminated SGR at byte %d", i)
			}
			end += i + 2
			applyScreenSGR(t, frame[i+2:end], &fg, &bg, &reverse)
			i = end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(frame[i:])
		if r == utf8.RuneError && size == 1 {
			t.Fatalf("invalid UTF-8 at byte %d", i)
		}
		i += size
		switch r {
		case '\n':
			x, y = 0, y+1
			continue
		case '\r':
			x = 0
			continue
		}
		if r < ' ' {
			t.Fatalf("unexpected control character %U at (%d,%d)", r, x, y)
		}
		cols := runewidth.RuneWidth(r)
		if cols == 0 {
			continue // combining mark belongs to the preceding terminal cell
		}
		if y >= height || x+cols > width {
			t.Fatalf("themed frame exceeds %dx%d at (%d,%d) for %q", width, height, x, y, r)
		}
		for col := 0; col < cols; col++ {
			cell := screenCell{char: r, fg: fg, bg: bg, reverse: reverse, seen: true}
			if reverse {
				cell.fg, cell.bg = cell.bg, cell.fg
			}
			s.rows[y][x+col] = cell
		}
		x += cols
	}
	return s
}

func applyScreenSGR(t *testing.T, sequence string, fg, bg *string, reverse *bool) {
	t.Helper()
	if sequence == "" {
		*fg, *bg, *reverse = "", "", false
		return
	}
	parts := strings.Split(sequence, ";")
	for i := 0; i < len(parts); i++ {
		code, err := strconv.Atoi(parts[i])
		if err != nil {
			t.Fatalf("unsupported SGR %q", sequence)
		}
		switch code {
		case 0:
			*fg, *bg, *reverse = "", "", false
		case 7:
			*reverse = true
		case 27:
			*reverse = false
		case 39:
			*fg = ""
		case 49:
			*bg = ""
		case 38, 48:
			if i+2 >= len(parts) {
				t.Fatalf("incomplete color SGR %q", sequence)
			}
			mode, err := strconv.Atoi(parts[i+1])
			if err != nil {
				t.Fatalf("invalid color SGR %q", sequence)
			}
			var color string
			switch mode {
			case 2:
				if i+4 >= len(parts) {
					t.Fatalf("incomplete RGB SGR %q", sequence)
				}
				var rgb [3]int
				for j := range rgb {
					rgb[j], err = strconv.Atoi(parts[i+2+j])
					if err != nil || rgb[j] < 0 || rgb[j] > 255 {
						t.Fatalf("invalid RGB SGR %q", sequence)
					}
				}
				color = fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2])
				i += 4
			case 5:
				color = "index:" + parts[i+2]
				i += 2
			default:
				t.Fatalf("unsupported color SGR %q", sequence)
			}
			if code == 38 {
				*fg = color
			} else {
				*bg = color
			}
		case 1, 2, 3, 4, 9, 22, 23, 24, 29:
			// Text attributes do not alter the stored colors.
		default:
			if code >= 30 && code <= 37 || code >= 90 && code <= 97 {
				*fg = fmt.Sprintf("ansi:%d", code)
			} else if code >= 40 && code <= 47 || code >= 100 && code <= 107 {
				*bg = fmt.Sprintf("ansi:%d", code)
			} else {
				t.Fatalf("unsupported SGR %q", sequence)
			}
		}
	}
}

func (s themeScreen) assertBackground(t *testing.T, want string, entireFrame bool) {
	t.Helper()
	seen := 0
	for y, row := range s.rows {
		for x, cell := range row {
			if !cell.seen && !entireFrame {
				continue
			}
			seen++
			if cell.bg != want {
				t.Fatalf("background mismatch at (%d,%d) on %q: got %q, want %q", x, y, cell.char, cell.bg, want)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no rendered terminal cells")
	}
}

// Finder matches intentionally reverse foreground and background. Everything
// outside those marked cells must still use the chosen frame background.
func (s themeScreen) assertBackgroundOutsideReverse(t *testing.T, want string) {
	t.Helper()
	reversed := 0
	for y, row := range s.rows {
		for x, cell := range row {
			if cell.reverse {
				reversed++
				if cell.bg == want {
					t.Fatalf("reverse highlight at (%d,%d) has no visible contrast", x, y)
				}
				continue
			}
			if cell.bg != want {
				t.Fatalf("background mismatch outside reverse highlight at (%d,%d): got %q, want %q", x, y, cell.bg, want)
			}
		}
	}
	if reversed == 0 {
		t.Fatal("no reverse-video match cells rendered")
	}
}

func (s themeScreen) assertTextColor(t *testing.T, label, want string) (int, int) {
	t.Helper()
	needle := []rune(label)
	for y, row := range s.rows {
		for x := 0; x+len(needle) <= s.width; x++ {
			matched := true
			for i, r := range needle {
				if row[x+i].char != r || !row[x+i].seen {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
			for i := range needle {
				if row[x+i].fg != want {
					t.Fatalf("%q foreground at (%d,%d): got %q, want %q", label, x+i, y, row[x+i].fg, want)
				}
			}
			return x, y
		}
	}
	t.Fatalf("%q not found in rendered frame", label)
	return 0, 0
}
