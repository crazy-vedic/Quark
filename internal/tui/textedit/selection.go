// Package textedit owns selection state and selection/edit key handling for
// single-line text widgets. It knows nothing about the TUI model: widgets are
// reached through Backend, and all positions are rune offsets.
package textedit

// Selection is an anchor/cursor pair in rune offsets. The anchor stays put
// while the user extends the selection; the cursor is the moving end.
type Selection struct {
	Anchor int
	Cursor int
}

// Active reports whether a non-empty selection exists.
func (s Selection) Active() bool { return s.Anchor != s.Cursor }

// Range returns the selection as [start, end) in ascending order.
func (s Selection) Range() (start, end int) {
	if s.Anchor <= s.Cursor {
		return s.Anchor, s.Cursor
	}
	return s.Cursor, s.Anchor
}

// Collapse removes the selection and places both ends at pos.
func (s *Selection) Collapse(pos int) {
	s.Anchor, s.Cursor = pos, pos
}

// SelectRange sets the selection to [start, end) with the cursor at end.
func (s *Selection) SelectRange(start, end int) {
	s.Anchor, s.Cursor = start, end
}

// SelectAll covers the whole text of the given rune length.
func (s *Selection) SelectAll(length int) {
	s.Anchor, s.Cursor = 0, length
}
