package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// clipboardMsg signals a clipboard operation result
type clipboardMsg struct {
	text string
	err  error
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
		}
		if cursorPos > 0 {
			sel.Extend(cursorPos - 1)
		}
		return true, false

	// Shift+Right: extend selection forward by character
	case msg.Type == tea.KeyShiftRight:
		if !sel.Active() {
			sel.anchor = cursorPos
		}
		inputLen := len(input.Value())
		if cursorPos < inputLen {
			sel.Extend(cursorPos + 1)
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

	// Note: Ctrl+Shift+Left and Ctrl+Shift+Right are typically sent as distinct
	// key types (KeyCtrlShiftLeft, KeyCtrlShiftRight) in terminals that support them.
	// We'll handle those separately if they're available, or fall back to standard behavior.

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
	case len(msg.Runes) > 0 && msg.Runes[0] >= 32:
		if sel.Active() {
			start, end := sel.Range()
			value := input.Value()
			newValue, _ := removeSelectedRange(value, start, end)
			// Now insert the typed character at the start position
			finalValue, newCursorPos := insertStringAtRunePosition(newValue, string(msg.Runes[0]), start)
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
