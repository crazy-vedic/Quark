package textedit

import tea "github.com/charmbracelet/bubbletea"

// Backend is what the editor needs from a text widget. Positions are rune
// offsets into Text(). The editor never stores a Backend, so a widget can
// live by value in a model that is copied on every update.
type Backend interface {
	Text() string
	SetText(s string)
	Cursor() int
	SetCursor(pos int)
	// Update forwards a message to the widget (typing, arrows, deletes).
	Update(msg tea.Msg) tea.Cmd
}

// lineBackend is implemented by widgets with more than one line. Single-line
// widgets do not implement it, and the helpers below treat them as one line.
type lineBackend interface {
	// MultiLine reports whether Enter inserts a newline.
	MultiLine() bool
	// MoveVertical moves the cursor one visual row up (delta < 0) or down
	// (delta > 0) and returns the new cursor offset.
	MoveVertical(delta int) int
}

func multiLine(b Backend) bool {
	lb, ok := b.(lineBackend)
	return ok && lb.MultiLine()
}

func moveVertical(b Backend, delta int) int {
	if lb, ok := b.(lineBackend); ok {
		return lb.MoveVertical(delta)
	}
	return b.Cursor()
}
