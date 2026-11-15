package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
)

func makeKeyMsg(keyStr string) tea.KeyMsg {
	msg := tea.KeyMsg{}
	switch keyStr {
	case "shift+left":
		msg.Type = tea.KeyShiftLeft
	case "shift+right":
		msg.Type = tea.KeyShiftRight
	case "shift+home":
		msg.Type = tea.KeyShiftHome
	case "shift+end":
		msg.Type = tea.KeyShiftEnd
	case "ctrl+shift+left":
		msg.Type = tea.KeyCtrlLeft
		// Note: tea.KeyMsg doesn't have a Shift field; handle this differently
		// For now, we'll test Shift+Home/End with real keys and skip Ctrl+Shift
	case "ctrl+shift+right":
		msg.Type = tea.KeyCtrlRight
	}
	return msg
}

func TestHandleSelectionKeyShiftArrows(t *testing.T) {
	tests := []struct {
		name              string
		keyStr            string
		initialValue      string
		cursorPos         int
		initialSelection  textSelection
		wantSelStart      int
		wantSelEnd        int
		shouldHandle      bool
		shouldComponent   bool
	}{
		{
			name:             "shift+left from middle",
			keyStr:           "shift+left",
			initialValue:     "hello",
			cursorPos:        3,
			initialSelection: textSelection{anchor: 3, cursor: 3},
			wantSelStart:     2,
			wantSelEnd:       3,
			shouldHandle:     true,
			shouldComponent:  false,
		},
		{
			name:             "shift+right from start",
			keyStr:           "shift+right",
			initialValue:     "hello",
			cursorPos:        0,
			initialSelection: textSelection{anchor: 0, cursor: 0},
			wantSelStart:     0,
			wantSelEnd:       1,
			shouldHandle:     true,
			shouldComponent:  false,
		},
		{
			name:             "shift+home from middle",
			keyStr:           "shift+home",
			initialValue:     "hello world",
			cursorPos:        6,
			initialSelection: textSelection{anchor: 6, cursor: 6},
			wantSelStart:     0,
			wantSelEnd:       6,
			shouldHandle:     true,
			shouldComponent:  false,
		},
		{
			name:             "shift+end from start",
			keyStr:           "shift+end",
			initialValue:     "hello",
			cursorPos:        0,
			initialSelection: textSelection{anchor: 0, cursor: 0},
			wantSelStart:     0,
			wantSelEnd:       5,
			shouldHandle:     true,
			shouldComponent:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := textinput.New()
			input.SetValue(tt.initialValue)
			input.SetCursor(tt.cursorPos)

			sel := tt.initialSelection
			msg := makeKeyMsg(tt.keyStr)

			handled, shouldComponent := handleSelectionKeyForInput(msg, &sel, &input, tt.cursorPos)

			selStart, selEnd := sel.Range()
			if selStart != tt.wantSelStart || selEnd != tt.wantSelEnd {
				t.Errorf("Selection range: got (%d, %d), want (%d, %d)",
					selStart, selEnd, tt.wantSelStart, tt.wantSelEnd)
			}
			if handled != tt.shouldHandle {
				t.Errorf("Handled: got %v, want %v", handled, tt.shouldHandle)
			}
			if shouldComponent != tt.shouldComponent {
				t.Errorf("ShouldComponent: got %v, want %v", shouldComponent, tt.shouldComponent)
			}
		})
	}
}

func TestHandleSelectionKeyCtrlA(t *testing.T) {
	input := textinput.New()
	input.SetValue("hello world")
	sel := textSelection{}

	msg := tea.KeyMsg{Type: tea.KeyCtrlA}
	handled, shouldComponent := handleSelectionKeyForInput(msg, &sel, &input, 5)

	start, end := sel.Range()
	if start != 0 || end != 11 {
		t.Errorf("Ctrl+A selection: got (%d, %d), want (0, 11)", start, end)
	}
	if !handled || shouldComponent {
		t.Errorf("Ctrl+A should be handled and not delegate to component")
	}
}

func TestHandleSelectionKeyBackspaceWithSelection(t *testing.T) {
	input := textinput.New()
	input.SetValue("hello world")
	sel := textSelection{anchor: 0, cursor: 5} // Select "hello"

	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	handled, _ := handleSelectionKeyForInput(msg, &sel, &input, 5)

	if input.Value() != " world" {
		t.Errorf("Backspace with selection: got %q, want %q", input.Value(), " world")
	}
	if !handled {
		t.Errorf("Backspace with selection should be handled")
	}
	if sel.Active() {
		t.Errorf("Selection should be cleared after backspace")
	}
}

func TestHandleSelectionKeyRegularCharacterWithSelection(t *testing.T) {
	input := textinput.New()
	input.SetValue("hello world")
	sel := textSelection{anchor: 0, cursor: 5} // Select "hello"

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}}
	handled, _ := handleSelectionKeyForInput(msg, &sel, &input, 5)

	if input.Value() != "X world" {
		t.Errorf("Type with selection: got %q, want %q", input.Value(), "X world")
	}
	if !handled {
		t.Errorf("Typing with selection should be handled")
	}
	if sel.Active() {
		t.Errorf("Selection should be cleared after typing")
	}
}

func TestInsertStringAtRunePosition(t *testing.T) {
	tests := []struct {
		name      string
		s         string
		text      string
		runePos   int
		wantStr   string
		wantCursor int
	}{
		{
			name:       "insert at start",
			s:          "world",
			text:       "hello ",
			runePos:    0,
			wantStr:    "hello world",
			wantCursor: 6,
		},
		{
			name:       "insert at end",
			s:          "hello",
			text:       " world",
			runePos:    5,
			wantStr:    "hello world",
			wantCursor: 11,
		},
		{
			name:       "insert in middle",
			s:          "heo",
			text:       "ll",
			runePos:    1,
			wantStr:    "hlleo",
			wantCursor: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStr, gotCursor := insertStringAtRunePosition(tt.s, tt.text, tt.runePos)
			if gotStr != tt.wantStr || gotCursor != tt.wantCursor {
				t.Errorf("insertStringAtRunePosition(%q, %q, %d) = (%q, %d), want (%q, %d)",
					tt.s, tt.text, tt.runePos, gotStr, gotCursor, tt.wantStr, tt.wantCursor)
			}
		})
	}
}
