package tui

import (
	"testing"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

func TestIsWordChar(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'a', true},
		{'Z', true},
		{'0', true},
		{'9', true},
		{'_', true},
		{'-', false}, // hyphen is NOT a word char; it's a separator
		{' ', false},
		{'.', false},
		{',', false},
		{':', false},
		{'/', false},
		{'"', false},
		{'€', false},
		// Japanese hiragana: unicode.IsLetter returns true, acceptable for word chars
		{'あ', true},
	}
	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			if got := textedit.IsWordChar(tt.r); got != tt.want {
				t.Errorf("textedit.IsWordChar(%q) = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}

func TestWordBoundsAt(t *testing.T) {
	tests := []struct {
		name      string
		runes     []rune
		pos       int
		wantStart int
		wantEnd   int
	}{
		{
			name:      "empty string",
			runes:     []rune{},
			pos:       0,
			wantStart: 0,
			wantEnd:   0,
		},
		{
			name:      "single letter",
			runes:     []rune("a"),
			pos:       0,
			wantStart: 0,
			wantEnd:   1,
		},
		{
			name:      "word in middle",
			runes:     []rune("hello world"),
			pos:       1,
			wantStart: 0,
			wantEnd:   5,
		},
		{
			name:      "word at end",
			runes:     []rune("hello world"),
			pos:       10,
			wantStart: 6,
			wantEnd:   11,
		},
		{
			name:      "space between words",
			runes:     []rune("hello world"),
			pos:       5,
			wantStart: 5,
			wantEnd:   6, // space is non-word
		},
		{
			name:      "underscore word",
			runes:     []rune("snake_case"),
			pos:       3,
			wantStart: 0,
			wantEnd:   10, // underscore is word char
		},
		{
			name:      "hyphenated",
			runes:     []rune("Content-Type"),
			pos:       0,
			wantStart: 0,
			wantEnd:   7, // hyphen is NOT word char; selects just "Content"
		},
		{
			name:      "hyphenated after hyphen",
			runes:     []rune("Content-Type"),
			pos:       8,
			wantStart: 8,
			wantEnd:   12, // selects just "Type"
		},
		{
			name:      "punctuation only",
			runes:     []rune("..."),
			pos:       1,
			wantStart: 1,
			wantEnd:   2,
		},
		{
			name:      "pos at end of string",
			runes:     []rune("hello"),
			pos:       5,
			wantStart: 0,
			wantEnd:   5, // backs up to last char
		},
		{
			name:      "camelCase",
			runes:     []rune("camelCase"),
			pos:       3,
			wantStart: 0,
			wantEnd:   9, // no case-sensitivity split
		},
		{
			name:      "number in word",
			runes:     []rune("test123abc"),
			pos:       5,
			wantStart: 0,
			wantEnd:   10, // numbers are word chars
		},
		{
			name:      "negative pos",
			runes:     []rune("hello"),
			pos:       -1,
			wantStart: 0,
			wantEnd:   5,
		},
		{
			name:      "pos beyond string",
			runes:     []rune("hello"),
			pos:       100,
			wantStart: 0,
			wantEnd:   5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := wordBoundsAt(tt.runes, tt.pos)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("wordBoundsAt(%v, %d) = (%d, %d), want (%d, %d)",
					string(tt.runes), tt.pos, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestNextWordBoundary(t *testing.T) {
	tests := []struct {
		name  string
		runes []rune
		pos   int
		want  int
	}{
		{
			name:  "from start of word",
			runes: []rune("hello world"),
			pos:   0,
			want:  6, // end of "hello" + skip space
		},
		{
			name:  "from middle of word",
			runes: []rune("hello world"),
			pos:   2,
			want:  6, // end of word + skip space
		},
		{
			name:  "at space",
			runes: []rune("hello world"),
			pos:   5,
			want:  6, // skip space to start of next word
		},
		{
			name:  "at start of second word",
			runes: []rune("hello world"),
			pos:   6,
			want:  11, // end of "world"
		},
		{
			name:  "at end",
			runes: []rune("hello"),
			pos:   5,
			want:  5, // no movement
		},
		{
			name:  "negative pos",
			runes: []rune("hello"),
			pos:   -1,
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := textedit.NextWordBoundary(tt.runes, tt.pos)
			if got != tt.want {
				t.Errorf("textedit.NextWordBoundary(%v, %d) = %d, want %d",
					string(tt.runes), tt.pos, got, tt.want)
			}
		})
	}
}

func TestPrevWordBoundary(t *testing.T) {
	tests := []struct {
		name  string
		runes []rune
		pos   int
		want  int
	}{
		{
			name:  "from end of word",
			runes: []rune("hello world"),
			pos:   5,
			want:  0, // start of "hello"
		},
		{
			name:  "from middle of word",
			runes: []rune("hello world"),
			pos:   3,
			want:  0,
		},
		{
			name:  "at space",
			runes: []rune("hello world"),
			pos:   6,
			want:  0, // skip back to start of "hello"
		},
		{
			name:  "at start of second word",
			runes: []rune("hello world"),
			pos:   6,
			want:  0,
		},
		{
			name:  "at start",
			runes: []rune("hello"),
			pos:   0,
			want:  0, // no movement
		},
		{
			name:  "from end",
			runes: []rune("hello world"),
			pos:   11,
			want:  6, // start of "world"
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := textedit.PrevWordBoundary(tt.runes, tt.pos)
			if got != tt.want {
				t.Errorf("textedit.PrevWordBoundary(%v, %d) = %d, want %d",
					string(tt.runes), tt.pos, got, tt.want)
			}
		})
	}
}
