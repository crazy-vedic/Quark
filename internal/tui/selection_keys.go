package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// clipboardMsg signals a successful clipboard copy
type clipboardMsg struct {
	text string
}

// handleSelectionKeyForInput processes a keyboard message for a text input field
// with selection support. It updates the selection and input state based on the key,
// and returns whether the key was handled (true if selection-related, false to pass
// to bubbles component).
//
// This function handles:
// - Shift+Left/Right: extend/shrink selection by character
// - Ctrl+Shift+Left/Right: extend/shrink selection by word
// - Shift+Home/End: extend selection to line boundaries
// - Ctrl+A: select all
// - Ctrl+C: copy selection to clipboard
// - Ctrl+X: cut selection (copy and remove)
// - Arrow keys without Shift: clear selection and move cursor
// - Regular characters: replace selection
// - Backspace/Delete with selection: remove selection
//
// Returns (handled, shouldUpdateComponent):
// - handled: true if this is a selection-related key
// - shouldUpdateComponent: true if bubbles component should be updated with the key
func handleSelectionKeyForInput(
	msg tea.KeyMsg,
	sel *textSelection,
	input *textinput.Model,
	cursorPos int,
) (handled bool, shouldUpdateComponent bool) {
	switch {
	// Shift+Left: extend selection backward by character
	case msg.Type == tea.KeyShiftLeft:
		if !sel.Active() {
			sel.anchor = cursorPos
			// Start extending from current cursor position
			sel.cursor = cursorPos
		}
		// Use the selection's cursor position as source of truth
		if sel.cursor > 0 {
			sel.Extend(sel.cursor - 1)
		}
		return true, false

	// Shift+Right: extend selection forward by character
	case msg.Type == tea.KeyShiftRight:
		if !sel.Active() {
			sel.anchor = cursorPos
			// Start extending from current cursor position
			sel.cursor = cursorPos
		}
		inputLen := len(input.Value())
		// Use the selection's cursor position as source of truth
		if sel.cursor < inputLen {
			sel.Extend(sel.cursor + 1)
		}
		return true, false

	// Shift+Home: extend selection to line start
	case msg.Type == tea.KeyShiftHome:
		if !sel.Active() {
			sel.anchor = cursorPos
		}
		sel.Extend(0)
		return true, false

	// Shift+End: extend selection to line end
	case msg.Type == tea.KeyShiftEnd:
		if !sel.Active() {
			sel.anchor = cursorPos
		}
		sel.Extend(len(input.Value()))
		return true, false

	// Ctrl+Shift+Left: extend selection backward by word boundary
	case msg.Type == tea.KeyCtrlShiftLeft:
		if !sel.Active() {
			sel.anchor = cursorPos
			sel.cursor = cursorPos
		}
		runes := stringToRunes(input.Value())
		prevBound := prevWordBoundary(runes, sel.cursor)
		sel.Extend(prevBound)
		return true, false

	// Ctrl+Shift+Right: extend selection forward by word boundary
	case msg.Type == tea.KeyCtrlShiftRight:
		if !sel.Active() {
			sel.anchor = cursorPos
			sel.cursor = cursorPos
		}
		runes := stringToRunes(input.Value())
		nextBound := nextWordBoundary(runes, sel.cursor)
		sel.Extend(nextBound)
		return true, false

	// Ctrl+A: select all
	case msg.Type == tea.KeyCtrlA:
		sel.SelectAll(len(input.Value()))
		return true, false

	// Arrow keys without Shift: clear selection and move cursor
	case msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight ||
		msg.Type == tea.KeyHome || msg.Type == tea.KeyEnd ||
		msg.Type == tea.KeyCtrlLeft || msg.Type == tea.KeyCtrlRight:
		sel.Clear(cursorPos)
		return false, true // Let bubbles handle movement

	// Backspace/Delete: if selection active, remove it; otherwise pass to bubbles
	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		if sel.Active() {
			start, end := sel.Range()
			value := input.Value()
			// Remove selected range
			newValue, newCursorPos := removeSelectedRange(value, start, end)
			input.SetValue(newValue)
			input.SetCursor(newCursorPos)
			sel.Clear(newCursorPos)
			return true, false
		}
		sel.Clear(cursorPos)
		return false, true // Let bubbles handle deletion

	// Regular printable characters: replace selection with typed character
	// Or paste operation: replace selection with full pasted content
	case len(msg.Runes) > 0 && (msg.Paste || msg.Runes[0] >= 32):
		if sel.Active() {
			start, end := sel.Range()
			value := input.Value()
			newValue, _ := removeSelectedRange(value, start, end)
			// Use all runes (for paste) or just first character (for typed input)
			textToInsert := string(msg.Runes)
			finalValue, newCursorPos := insertStringAtRunePosition(newValue, textToInsert, start)
			input.SetValue(finalValue)
			input.SetCursor(newCursorPos)
			sel.Clear(newCursorPos)
			return true, false
		}
		sel.Clear(cursorPos)
		return false, true // Let bubbles handle insertion

	default:
		sel.Clear(cursorPos)
		return false, true // Default: let bubbles handle the key
	}
}

// insertStringAtRunePosition inserts text at a rune position and returns
// the modified string and new cursor position (after inserted text).
func insertStringAtRunePosition(s string, text string, runePos int) (string, int) {
	runes := stringToRunes(s)
	textRunes := stringToRunes(text)
	if runePos < 0 {
		runePos = 0
	}
	if runePos > len(runes) {
		runePos = len(runes)
	}
	// Build result by concatenating: before + text + after
	result := make([]rune, 0, len(runes)+len(textRunes))
	result = append(result, runes[:runePos]...)
	result = append(result, textRunes...)
	result = append(result, runes[runePos:]...)
	return runesToString(result), runePos + len(textRunes)
}

// copySelectionText extracts the selected text.
// Returns empty string if no selection is active.
func copySelectionText(value string, sel textSelection) string {
	if !sel.Active() {
		return ""
	}
	start, end := sel.Range()
	runes := stringToRunes(value)
	if start < 0 || end > len(runes) || start > end {
		return ""
	}
	return runesToString(runes[start:end])
}

// cutSelection removes selected text and returns the new value and cursor position.
// Returns original value and cursor if no selection is active.
func cutSelection(value string, sel textSelection) (string, int) {
	if !sel.Active() {
		return value, 0
	}
	start, end := sel.Range()
	newValue, newCursorPos := removeSelectedRange(value, start, end)
	return newValue, newCursorPos
}

// handleSelectionKeyForTextarea is like handleSelectionKeyForInput but for textarea components.
// Textarea uses Cursor() instead of Position() to get the current cursor position.
//
// This function handles the same selection operations as handleSelectionKeyForInput:
// - Shift+Left/Right: extend/shrink selection by character
// - Ctrl+Shift+Left/Right: extend/shrink selection by word
// - Shift+Home/End: extend selection to line boundaries
// - Ctrl+A: select all
// - Arrow keys: clear selection
// - Backspace/Delete: remove selection or delegate
// - Regular characters: replace selection
//
// Returns (handled, shouldUpdateComponent):
// - handled: true if this is a selection-related key
// - shouldUpdateComponent: true if bubbles component should be updated with the key
func handleSelectionKeyForTextarea(
	msg tea.KeyMsg,
	sel *textSelection,
	ta *textarea.Model,
	cursorPos int,
) (handled bool, shouldUpdateComponent bool) {
	switch {
	// Shift+Left: extend selection backward by character
	case msg.Type == tea.KeyShiftLeft:
		if !sel.Active() {
			sel.anchor = cursorPos
			sel.cursor = cursorPos
		}
		if sel.cursor > 0 {
			sel.Extend(sel.cursor - 1)
		}
		return true, false

	// Shift+Right: extend selection forward by character
	case msg.Type == tea.KeyShiftRight:
		if !sel.Active() {
			sel.anchor = cursorPos
			sel.cursor = cursorPos
		}
		taLen := len(ta.Value())
		if sel.cursor < taLen {
			sel.Extend(sel.cursor + 1)
		}
		return true, false

	// Shift+Home: extend selection to line start
	case msg.Type == tea.KeyShiftHome:
		if !sel.Active() {
			sel.anchor = cursorPos
		}
		sel.Extend(0)
		return true, false

	// Shift+End: extend selection to line end
	case msg.Type == tea.KeyShiftEnd:
		if !sel.Active() {
			sel.anchor = cursorPos
		}
		sel.Extend(len(ta.Value()))
		return true, false

	// Ctrl+Shift+Left: extend selection backward by word boundary
	case msg.Type == tea.KeyCtrlShiftLeft:
		if !sel.Active() {
			sel.anchor = cursorPos
			sel.cursor = cursorPos
		}
		runes := stringToRunes(ta.Value())
		prevBound := prevWordBoundary(runes, sel.cursor)
		sel.Extend(prevBound)
		return true, false

	// Ctrl+Shift+Right: extend selection forward by word boundary
	case msg.Type == tea.KeyCtrlShiftRight:
		if !sel.Active() {
			sel.anchor = cursorPos
			sel.cursor = cursorPos
		}
		runes := stringToRunes(ta.Value())
		nextBound := nextWordBoundary(runes, sel.cursor)
		sel.Extend(nextBound)
		return true, false

	// Ctrl+A: select all
	case msg.Type == tea.KeyCtrlA:
		sel.SelectAll(len(ta.Value()))
		return true, false

	// Arrow keys without Shift: clear selection and move cursor
	case msg.Type == tea.KeyLeft || msg.Type == tea.KeyRight ||
		msg.Type == tea.KeyHome || msg.Type == tea.KeyEnd ||
		msg.Type == tea.KeyCtrlLeft || msg.Type == tea.KeyCtrlRight ||
		msg.Type == tea.KeyUp || msg.Type == tea.KeyDown:
		sel.Clear(cursorPos)
		return false, true // Let bubbles handle movement

	// Backspace/Delete: if selection active, remove it; otherwise pass to bubbles
	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		if sel.Active() {
			start, end := sel.Range()
			value := ta.Value()
			newValue, newCursorPos := removeSelectedRange(value, start, end)
			ta.SetValue(newValue)
			ta.SetCursor(newCursorPos)
			sel.Clear(newCursorPos)
			return true, false
		}
		sel.Clear(cursorPos)
		return false, true // Let bubbles handle deletion

	// Regular printable characters: replace selection with typed character
	// Or paste operation: replace selection with full pasted content
	case len(msg.Runes) > 0 && (msg.Paste || msg.Runes[0] >= 32):
		if sel.Active() {
			start, end := sel.Range()
			value := ta.Value()
			newValue, _ := removeSelectedRange(value, start, end)
			textToInsert := string(msg.Runes)
			finalValue, newCursorPos := insertStringAtRunePosition(newValue, textToInsert, start)
			ta.SetValue(finalValue)
			ta.SetCursor(newCursorPos)
			sel.Clear(newCursorPos)
			return true, false
		}
		sel.Clear(cursorPos)
		return false, true // Let bubbles handle insertion

	default:
		sel.Clear(cursorPos)
		return false, true // Default: let bubbles handle the key
	}
}
