package tui

import "testing"

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
		{'-', true},
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
			if got := isWordChar(tt.r); got != tt.want {
				t.Errorf("isWordChar(%q) = %v, want %v", tt.r, got, tt.want)
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
			wantEnd:   12, // hyphen is word char
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

func TestByteOffsetToRuneOffset(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		byteOff int
		want    int
	}{
		{
			name:    "zero offset",
			s:       "hello",
			byteOff: 0,
			want:    0,
		},
		{
			name:    "mid-string ASCII",
			s:       "hello",
			byteOff: 2,
			want:    2,
		},
		{
			name:    "end of ASCII string",
			s:       "hello",
			byteOff: 5,
			want:    5,
		},
		{
			name:    "UTF-8 multi-byte",
			s:       "hello世界",
			byteOff: 5, // after "hello"
			want:    5,
		},
		{
			name:    "negative offset",
			s:       "hello",
			byteOff: -1,
			want:    0,
		},
		{
			name:    "beyond string",
			s:       "hello",
			byteOff: 100,
			want:    5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := byteOffsetToRuneOffset(tt.s, tt.byteOff)
			if got != tt.want {
				t.Errorf("byteOffsetToRuneOffset(%q, %d) = %d, want %d",
					tt.s, tt.byteOff, got, tt.want)
			}
		})
	}
}

func TestRuneOffsetToByteOffset(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		runeOff int
		want    int
	}{
		{
			name:    "zero offset",
			s:       "hello",
			runeOff: 0,
			want:    0,
		},
		{
			name:    "mid-string",
			s:       "hello",
			runeOff: 2,
			want:    2,
		},
		{
			name:    "end of string",
			s:       "hello",
			runeOff: 5,
			want:    5,
		},
		{
			name:    "negative offset",
			s:       "hello",
			runeOff: -1,
			want:    0,
		},
		{
			name:    "beyond string",
			s:       "hello",
			runeOff: 100,
			want:    5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runeOffsetToByteOffset(tt.s, tt.runeOff)
			if got != tt.want {
				t.Errorf("runeOffsetToByteOffset(%q, %d) = %d, want %d",
					tt.s, tt.runeOff, got, tt.want)
			}
		})
	}
}

func TestSelectedText(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		start int
		end   int
		want  string
	}{
		{
			name:  "basic selection",
			s:     "hello world",
			start: 0,
			end:   5,
			want:  "hello",
		},
		{
			name:  "middle selection",
			s:     "hello world",
			start: 6,
			end:   11,
			want:  "world",
		},
		{
			name:  "full string",
			s:     "hello",
			start: 0,
			end:   5,
			want:  "hello",
		},
		{
			name:  "empty selection",
			s:     "hello",
			start: 2,
			end:   2,
			want:  "",
		},
		{
			name:  "invalid range (start > end)",
			s:     "hello",
			start: 5,
			end:   2,
			want:  "",
		},
		{
			name:  "out of bounds",
			s:     "hello",
			start: 0,
			end:   100,
			want:  "hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectedText(tt.s, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("selectedText(%q, %d, %d) = %q, want %q",
					tt.s, tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestRemoveSelectedRange(t *testing.T) {
	tests := []struct {
		name      string
		s         string
		start     int
		end       int
		wantStr   string
		wantCurse int // cursor position after removal
	}{
		{
			name:      "remove from start",
			s:         "hello world",
			start:     0,
			end:       6,
			wantStr:   "world",
			wantCurse: 0,
		},
		{
			name:      "remove from end",
			s:         "hello world",
			start:     6,
			end:       11,
			wantStr:   "hello ",
			wantCurse: 6,
		},
		{
			name:      "remove middle",
			s:         "hello world test",
			start:     5,
			end:       11,
			wantStr:   "hello test",
			wantCurse: 5,
		},
		{
			name:      "remove all",
			s:         "hello",
			start:     0,
			end:       5,
			wantStr:   "",
			wantCurse: 0,
		},
		{
			name:      "empty range",
			s:         "hello",
			start:     2,
			end:       2,
			wantStr:   "hello",
			wantCurse: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStr, gotCur := removeSelectedRange(tt.s, tt.start, tt.end)
			if gotStr != tt.wantStr || gotCur != tt.wantCurse {
				t.Errorf("removeSelectedRange(%q, %d, %d) = (%q, %d), want (%q, %d)",
					tt.s, tt.start, tt.end, gotStr, gotCur, tt.wantStr, tt.wantCurse)
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
			got := nextWordBoundary(tt.runes, tt.pos)
			if got != tt.want {
				t.Errorf("nextWordBoundary(%v, %d) = %d, want %d",
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
			got := prevWordBoundary(tt.runes, tt.pos)
			if got != tt.want {
				t.Errorf("prevWordBoundary(%v, %d) = %d, want %d",
					string(tt.runes), tt.pos, got, tt.want)
			}
		})
	}
}
