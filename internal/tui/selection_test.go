package tui

import "testing"

func TestTextSelectionActive(t *testing.T) {
	tests := []struct {
		name      string
		sel       textSelection
		wantActive bool
	}{
		{
			name:      "empty selection",
			sel:       textSelection{anchor: 0, cursor: 0},
			wantActive: false,
		},
		{
			name:      "forward selection",
			sel:       textSelection{anchor: 0, cursor: 5},
			wantActive: true,
		},
		{
			name:      "backward selection",
			sel:       textSelection{anchor: 5, cursor: 0},
			wantActive: true,
		},
		{
			name:      "same position",
			sel:       textSelection{anchor: 3, cursor: 3},
			wantActive: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sel.Active(); got != tt.wantActive {
				t.Errorf("Active() = %v, want %v", got, tt.wantActive)
			}
		})
	}
}

func TestTextSelectionRange(t *testing.T) {
	tests := []struct {
		name      string
		sel       textSelection
		wantStart int
		wantEnd   int
	}{
		{
			name:      "forward selection",
			sel:       textSelection{anchor: 2, cursor: 5},
			wantStart: 2,
			wantEnd:   5,
		},
		{
			name:      "backward selection",
			sel:       textSelection{anchor: 5, cursor: 2},
			wantStart: 2,
			wantEnd:   5,
		},
		{
			name:      "zero-based forward",
			sel:       textSelection{anchor: 0, cursor: 3},
			wantStart: 0,
			wantEnd:   3,
		},
		{
			name:      "zero-based backward",
			sel:       textSelection{anchor: 3, cursor: 0},
			wantStart: 0,
			wantEnd:   3,
		},
		{
			name:      "same position",
			sel:       textSelection{anchor: 5, cursor: 5},
			wantStart: 5,
			wantEnd:   5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := tt.sel.Range()
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("Range() = (%d, %d), want (%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestTextSelectionClear(t *testing.T) {
	sel := textSelection{anchor: 2, cursor: 8}
	sel.Clear(5)

	if sel.anchor != 5 || sel.cursor != 5 {
		t.Errorf("Clear(5) = (%d, %d), want (5, 5)", sel.anchor, sel.cursor)
	}

	if sel.Active() {
		t.Errorf("Clear should deactivate selection")
	}
}

func TestTextSelectionExtend(t *testing.T) {
	tests := []struct {
		name      string
		initial   textSelection
		extendTo  int
		wantRange [2]int
	}{
		{
			name:      "extend forward from zero",
			initial:   textSelection{anchor: 0, cursor: 0},
			extendTo:  5,
			wantRange: [2]int{0, 5},
		},
		{
			name:      "extend backward from position",
			initial:   textSelection{anchor: 5, cursor: 5},
			extendTo:  2,
			wantRange: [2]int{2, 5},
		},
		{
			name:      "shrink from forward selection",
			initial:   textSelection{anchor: 0, cursor: 10},
			extendTo:  3,
			wantRange: [2]int{0, 3},
		},
		{
			name:      "reverse direction",
			initial:   textSelection{anchor: 0, cursor: 5},
			extendTo:  -2, // hypothetical; testing the logic
			wantRange: [2]int{-2, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.Extend(tt.extendTo)
			start, end := tt.initial.Range()
			if start != tt.wantRange[0] || end != tt.wantRange[1] {
				t.Errorf("After Extend(%d): Range() = (%d, %d), want (%d, %d)",
					tt.extendTo, start, end, tt.wantRange[0], tt.wantRange[1])
			}
		})
	}
}

func TestTextSelectionSelectAll(t *testing.T) {
	sel := textSelection{anchor: 3, cursor: 7}
	sel.SelectAll(20)

	start, end := sel.Range()
	if start != 0 || end != 20 {
		t.Errorf("SelectAll(20): Range() = (%d, %d), want (0, 20)", start, end)
	}

	if !sel.Active() {
		t.Errorf("SelectAll should create an active selection")
	}
}

func TestTextSelectionSelectWord(t *testing.T) {
	sel := textSelection{}
	sel.SelectWord(5, 10)

	start, end := sel.Range()
	if start != 5 || end != 10 {
		t.Errorf("SelectWord(5, 10): Range() = (%d, %d), want (5, 10)", start, end)
	}

	if !sel.Active() {
		t.Errorf("SelectWord should create an active selection")
	}
}

func TestTextSelectionMoveToSelectionEnd(t *testing.T) {
	tests := []struct {
		name      string
		initial   textSelection
		moveTo    int
		wantAnchor int
		wantCursor int
	}{
		{
			name:       "move to position",
			initial:    textSelection{anchor: 0, cursor: 5},
			moveTo:     3,
			wantAnchor: 3,
			wantCursor: 3,
		},
		{
			name:       "move from backward selection",
			initial:    textSelection{anchor: 10, cursor: 2},
			moveTo:     6,
			wantAnchor: 6,
			wantCursor: 6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.MoveToSelectionEnd(tt.moveTo)
			if tt.initial.anchor != tt.wantAnchor || tt.initial.cursor != tt.wantCursor {
				t.Errorf("After MoveToSelectionEnd(%d): (%d, %d), want (%d, %d)",
					tt.moveTo, tt.initial.anchor, tt.initial.cursor, tt.wantAnchor, tt.wantCursor)
			}
		})
	}
}
