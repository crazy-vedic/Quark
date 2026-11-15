package tui

// textSelection represents a text selection with an anchor and cursor position.
// In terminal editors, both positions are tracked in rune offsets to handle
// multi-byte Unicode correctly. The selection extends from the smaller to the
// larger of anchor and cursor.
type textSelection struct {
	anchor int // start of selection (does not move)
	cursor int // active end of selection (moves with arrow keys)
}

// Active returns true if a selection exists (anchor != cursor).
func (s textSelection) Active() bool {
	return s.anchor != s.cursor
}

// Range returns the start and end of the selection as [start, end).
// The returned values are always in ascending order.
func (s textSelection) Range() (start, end int) {
	if s.anchor <= s.cursor {
		return s.anchor, s.cursor
	}
	return s.cursor, s.anchor
}

// Clear removes the selection and moves both anchor and cursor to pos.
// This is called when the user presses a non-extending key (like Left without Shift).
func (s *textSelection) Clear(pos int) {
	s.anchor = pos
	s.cursor = pos
}

// Extend moves the cursor to newCursor without moving the anchor.
// This grows or shrinks the selection as the user holds Shift and presses arrow keys.
func (s *textSelection) Extend(newCursor int) {
	s.cursor = newCursor
}

// SelectAll sets the selection to cover the entire value [0, length).
func (s *textSelection) SelectAll(length int) {
	s.anchor = 0
	s.cursor = length
}

// SelectWord sets the selection to a word at the given position.
// start and end are the boundaries of the word in rune offsets.
func (s *textSelection) SelectWord(start, end int) {
	s.anchor = start
	s.cursor = end
}

// MoveToSelectionEnd moves to the active end of the selection and clears it.
// This is called when the user presses an arrow key without Shift after having
// a selection. It moves the cursor to the end (or beginning) of the selection
// that the arrow key would move towards.
func (s *textSelection) MoveToSelectionEnd(pos int) {
	s.anchor = pos
	s.cursor = pos
}
