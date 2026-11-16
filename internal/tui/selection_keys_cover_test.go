package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type selKeyCase struct {
	name        string
	msg         tea.KeyMsg
	value       string
	cursor      int
	sel         textSelection
	wantHandled bool
	wantUpdate  bool
	wantAnchor  int
	wantCursor  int
	wantValue   string
}

// selKeyCases covers every branch of the key dispatch shared by the input and
// textarea handlers. Textarea-only keys (Up/Down) are exercised separately.
var selKeyCases = []selKeyCase{
	{name: "shift+left at start is no-op", msg: tea.KeyMsg{Type: tea.KeyShiftLeft},
		value: "hello world", cursor: 0, sel: textSelection{}, wantHandled: true,
		wantAnchor: 0, wantCursor: 0, wantValue: "hello world"},
	{name: "shift+left from middle", msg: tea.KeyMsg{Type: tea.KeyShiftLeft},
		value: "hello world", cursor: 3, sel: textSelection{}, wantHandled: true,
		wantAnchor: 3, wantCursor: 2, wantValue: "hello world"},
	{
		name:        "shift+left extends active selection",
		msg:         tea.KeyMsg{Type: tea.KeyShiftLeft},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 5, cursor: 3},
		wantHandled: true,
		wantAnchor:  5,
		wantCursor:  2,
		wantValue:   "hello world",
	},
	{name: "shift+right at end is no-op", msg: tea.KeyMsg{Type: tea.KeyShiftRight},
		value: "hello world", cursor: 11, sel: textSelection{}, wantHandled: true,
		wantAnchor: 11, wantCursor: 11, wantValue: "hello world"},
	{name: "shift+right from middle", msg: tea.KeyMsg{Type: tea.KeyShiftRight},
		value: "hello world", cursor: 2, sel: textSelection{}, wantHandled: true,
		wantAnchor: 2, wantCursor: 3, wantValue: "hello world"},
	{name: "shift+home extends to start", msg: tea.KeyMsg{Type: tea.KeyShiftHome},
		value: "hello world", cursor: 5, sel: textSelection{}, wantHandled: true,
		wantAnchor: 5, wantCursor: 0, wantValue: "hello world"},
	{
		name:        "shift+home with active selection",
		msg:         tea.KeyMsg{Type: tea.KeyShiftHome},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 3, cursor: 4},
		wantHandled: true,
		wantAnchor:  3,
		wantCursor:  0,
		wantValue:   "hello world",
	},
	{name: "shift+end extends to end", msg: tea.KeyMsg{Type: tea.KeyShiftEnd},
		value: "hello world", cursor: 5, sel: textSelection{}, wantHandled: true,
		wantAnchor: 5, wantCursor: 11, wantValue: "hello world"},
	{
		name:        "shift+end with active selection",
		msg:         tea.KeyMsg{Type: tea.KeyShiftEnd},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 2, cursor: 3},
		wantHandled: true,
		wantAnchor:  2,
		wantCursor:  11,
		wantValue:   "hello world",
	},
	{name: "ctrl+shift+left by word", msg: tea.KeyMsg{Type: tea.KeyCtrlShiftLeft},
		value: "hello world", cursor: 11, sel: textSelection{}, wantHandled: true,
		wantAnchor: 11, wantCursor: 6, wantValue: "hello world"},
	{
		name:        "ctrl+shift+left with active selection",
		msg:         tea.KeyMsg{Type: tea.KeyCtrlShiftLeft},
		value:       "hello world",
		cursor:      8,
		sel:         textSelection{anchor: 8, cursor: 8},
		wantHandled: true,
		wantAnchor:  8,
		wantCursor:  6,
		wantValue:   "hello world",
	},
	{name: "ctrl+shift+right by word", msg: tea.KeyMsg{Type: tea.KeyCtrlShiftRight},
		value: "hello world", cursor: 0, sel: textSelection{}, wantHandled: true,
		wantAnchor: 0, wantCursor: 6, wantValue: "hello world"},
	{
		name:        "ctrl+shift+right with active selection",
		msg:         tea.KeyMsg{Type: tea.KeyCtrlShiftRight},
		value:       "hello world",
		cursor:      1,
		sel:         textSelection{anchor: 1, cursor: 1},
		wantHandled: true,
		wantAnchor:  1,
		wantCursor:  6,
		wantValue:   "hello world",
	},
	{name: "ctrl+a selects all", msg: tea.KeyMsg{Type: tea.KeyCtrlA},
		value: "hello world", cursor: 4, sel: textSelection{}, wantHandled: true,
		wantAnchor: 0, wantCursor: 11, wantValue: "hello world"},
	{
		name:        "left clears selection",
		msg:         tea.KeyMsg{Type: tea.KeyLeft},
		value:       "hello world",
		cursor:      4,
		sel:         textSelection{anchor: 1, cursor: 3},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  4,
		wantCursor:  4,
		wantValue:   "hello world",
	},
	{
		name:        "right clears selection",
		msg:         tea.KeyMsg{Type: tea.KeyRight},
		value:       "hello world",
		cursor:      4,
		sel:         textSelection{anchor: 1, cursor: 3},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  4,
		wantCursor:  4,
		wantValue:   "hello world",
	},
	{
		name:        "home clears selection",
		msg:         tea.KeyMsg{Type: tea.KeyHome},
		value:       "hello world",
		cursor:      4,
		sel:         textSelection{anchor: 1, cursor: 3},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  4,
		wantCursor:  4,
		wantValue:   "hello world",
	},
	{
		name:        "end clears selection",
		msg:         tea.KeyMsg{Type: tea.KeyEnd},
		value:       "hello world",
		cursor:      4,
		sel:         textSelection{anchor: 1, cursor: 3},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  4,
		wantCursor:  4,
		wantValue:   "hello world",
	},
	{
		name:        "ctrl+left clears selection",
		msg:         tea.KeyMsg{Type: tea.KeyCtrlLeft},
		value:       "hello world",
		cursor:      4,
		sel:         textSelection{anchor: 1, cursor: 3},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  4,
		wantCursor:  4,
		wantValue:   "hello world",
	},
	{
		name:        "ctrl+right clears selection",
		msg:         tea.KeyMsg{Type: tea.KeyCtrlRight},
		value:       "hello world",
		cursor:      4,
		sel:         textSelection{anchor: 1, cursor: 3},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  4,
		wantCursor:  4,
		wantValue:   "hello world",
	},
	{
		name:        "backspace removes selection",
		msg:         tea.KeyMsg{Type: tea.KeyBackspace},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 2, cursor: 5},
		wantHandled: true,
		wantAnchor:  2,
		wantCursor:  2,
		wantValue:   "he world",
	},
	{
		name:        "delete removes selection",
		msg:         tea.KeyMsg{Type: tea.KeyDelete},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 0, cursor: 6},
		wantHandled: true,
		wantAnchor:  0,
		wantCursor:  0,
		wantValue:   "world",
	},
	{name: "backspace without selection passes through", msg: tea.KeyMsg{Type: tea.KeyBackspace},
		value: "hello world", cursor: 3, sel: textSelection{}, wantHandled: false, wantUpdate: true,
		wantAnchor: 3, wantCursor: 3, wantValue: "hello world"},
	{
		name:        "typed char replaces selection",
		msg:         tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 0, cursor: 5},
		wantHandled: true,
		wantAnchor:  1,
		wantCursor:  1,
		wantValue:   "x world",
	},
	{
		name:        "paste replaces selection",
		msg:         tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab"), Paste: true},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 0, cursor: 5},
		wantHandled: true,
		wantAnchor:  2,
		wantCursor:  2,
		wantValue:   "ab world",
	},
	{
		name:        "typed char without selection passes through",
		msg:         tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  5,
		wantCursor:  5,
		wantValue:   "hello world",
	},
	{
		name:        "non-printable key falls to default",
		msg:         tea.KeyMsg{Type: tea.KeyEnter},
		value:       "hello world",
		cursor:      5,
		sel:         textSelection{anchor: 1, cursor: 3},
		wantHandled: false,
		wantUpdate:  true,
		wantAnchor:  5,
		wantCursor:  5,
		wantValue:   "hello world",
	},
}

func TestHandleSelectionKeyForInputAllBranches(t *testing.T) {
	for _, tc := range selKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			in := textinput.New()
			in.SetValue(tc.value)
			in.SetCursor(tc.cursor)
			sel := tc.sel
			handled, update := handleSelectionKeyForInput(tc.msg, &sel, &in, tc.cursor)
			if handled != tc.wantHandled || update != tc.wantUpdate {
				t.Fatalf(
					"handled=%v update=%v, want %v/%v",
					handled,
					update,
					tc.wantHandled,
					tc.wantUpdate,
				)
			}
			if sel.anchor != tc.wantAnchor || sel.cursor != tc.wantCursor {
				t.Errorf("selection anchor=%d cursor=%d, want %d/%d",
					sel.anchor, sel.cursor, tc.wantAnchor, tc.wantCursor)
			}
			if in.Value() != tc.wantValue {
				t.Errorf("value=%q, want %q", in.Value(), tc.wantValue)
			}
		})
	}
}

func TestHandleSelectionKeyForTextareaAllBranches(t *testing.T) {
	cases := append([]selKeyCase{}, selKeyCases...)
	cases = append(cases,
		selKeyCase{
			name:        "up clears selection",
			msg:         tea.KeyMsg{Type: tea.KeyUp},
			value:       "hello world",
			cursor:      4,
			sel:         textSelection{anchor: 1, cursor: 3},
			wantHandled: false,
			wantUpdate:  true,
			wantAnchor:  4,
			wantCursor:  4,
			wantValue:   "hello world",
		},
		selKeyCase{
			name:        "down clears selection",
			msg:         tea.KeyMsg{Type: tea.KeyDown},
			value:       "hello world",
			cursor:      4,
			sel:         textSelection{anchor: 1, cursor: 3},
			wantHandled: false,
			wantUpdate:  true,
			wantAnchor:  4,
			wantCursor:  4,
			wantValue:   "hello world",
		},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ta := textarea.New()
			ta.SetValue(tc.value)
			sel := tc.sel
			handled, update := handleSelectionKeyForTextarea(tc.msg, &sel, &ta, tc.cursor)
			if handled != tc.wantHandled || update != tc.wantUpdate {
				t.Fatalf(
					"handled=%v update=%v, want %v/%v",
					handled,
					update,
					tc.wantHandled,
					tc.wantUpdate,
				)
			}
			if sel.anchor != tc.wantAnchor || sel.cursor != tc.wantCursor {
				t.Errorf("selection anchor=%d cursor=%d, want %d/%d",
					sel.anchor, sel.cursor, tc.wantAnchor, tc.wantCursor)
			}
			if ta.Value() != tc.wantValue {
				t.Errorf("value=%q, want %q", ta.Value(), tc.wantValue)
			}
		})
	}
}

func TestSelectionHelperEdgeCases(t *testing.T) {
	t.Run("copy with no selection returns empty", func(t *testing.T) {
		if got := copySelectionText("hello", textSelection{anchor: 2, cursor: 2}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("copy with out-of-range selection returns empty", func(t *testing.T) {
		if got := copySelectionText("hi", textSelection{anchor: 0, cursor: 50}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("copy returns selected text", func(t *testing.T) {
		if got := copySelectionText("hello", textSelection{anchor: 1, cursor: 4}); got != "ell" {
			t.Errorf("got %q, want ell", got)
		}
	})
	t.Run("cut without selection is identity", func(t *testing.T) {
		v, c := cutSelection("hello", textSelection{anchor: 3, cursor: 3}, 3)
		if v != "hello" || c != 3 {
			t.Errorf("got %q,%d want hello,3", v, c)
		}
	})
	t.Run("cut with selection removes range", func(t *testing.T) {
		v, c := cutSelection("hello", textSelection{anchor: 1, cursor: 3}, 3)
		if v != "hlo" || c != 1 {
			t.Errorf("got %q,%d want hlo,1", v, c)
		}
	})
	t.Run("insert clamps negative position to start", func(t *testing.T) {
		v, c := insertStringAtRunePosition("abc", "X", -5)
		if v != "Xabc" || c != 1 {
			t.Errorf("got %q,%d want Xabc,1", v, c)
		}
	})
	t.Run("insert clamps overflow position to end", func(t *testing.T) {
		v, c := insertStringAtRunePosition("abc", "X", 99)
		if v != "abcX" || c != 4 {
			t.Errorf("got %q,%d want abcX,4", v, c)
		}
	})
}
