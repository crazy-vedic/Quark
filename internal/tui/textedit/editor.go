package textedit

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Editor holds the key bindings and clipboard for selection handling. It has
// no per-widget state: the selection and the backend are passed to each call.
type Editor struct {
	Keys Keymap
	Clip Clipboard
}

// New returns an Editor with the default keymap.
func New(clip Clipboard) Editor {
	return Editor{Keys: DefaultKeymap(), Clip: clip}
}

// HandleKey processes a key against the selection and backend. It returns
// handled=false for keys the editor does not own; the caller then runs its own
// logic (Enter, Tab, Ctrl+C with no selection) and calls Sync afterwards.
func (e Editor) HandleKey(sel *Selection, b Backend, msg tea.KeyMsg) (tea.Cmd, bool) {
	key := msg.String()
	k := e.Keys

	switch {
	case k.SelectAll.Match(key):
		sel.SelectAll(runeLen(b.Text()))
		b.SetCursor(sel.Cursor)
		return nil, true

	case k.Copy.Match(key):
		if !sel.Active() {
			return nil, false
		}
		return e.Clip.WriteCmd(selectedText(b.Text(), *sel)), true

	case k.Cut.Match(key):
		if !sel.Active() {
			return nil, true
		}
		text := selectedText(b.Text(), *sel)
		e.deleteSelection(sel, b)
		return e.Clip.WriteCmd(text), true

	case k.Paste.Match(key):
		// The read is asynchronous. The text returns as a PasteResultMsg and is
		// inserted when the application delivers it, so the UI is never blocked
		// on the clipboard.
		return e.Clip.ReadCmd(), true

	case k.SelectCharLeft.Match(key), k.SelectCharRight.Match(key),
		k.SelectWordLeft.Match(key), k.SelectWordRight.Match(key),
		k.SelectLineStart.Match(key), k.SelectLineEnd.Match(key),
		k.SelectLineUp.Match(key), k.SelectLineDown.Match(key):
		e.extend(sel, b, key)
		return nil, true

	case k.LineUp.Match(key), k.LineDown.Match(key):
		// Vertical moves belong to the widget unless a selection must collapse.
		if !sel.Active() {
			return nil, false
		}
		start, end := sel.Range()
		if k.LineUp.Match(key) {
			sel.Collapse(start)
		} else {
			sel.Collapse(end)
		}
		b.SetCursor(sel.Cursor)
		return nil, true

	case k.WordLeft.Match(key), k.WordRight.Match(key):
		// Word movement is computed here, not by the widget, so every backend
		// moves by the same rule regardless of its own keymap.
		if sel.Active() {
			start, end := sel.Range()
			if k.WordLeft.Match(key) {
				sel.Collapse(start)
			} else {
				sel.Collapse(end)
			}
			b.SetCursor(sel.Cursor)
			return nil, true
		}
		runes := []rune(b.Text())
		pos := b.Cursor()
		if k.WordLeft.Match(key) {
			pos = PrevWordBoundary(runes, pos)
		} else {
			pos = NextWordBoundary(runes, pos)
		}
		sel.Collapse(pos)
		b.SetCursor(pos)
		return nil, true

	case k.CharLeft.Match(key), k.CharRight.Match(key),
		k.LineStart.Match(key), k.LineEnd.Match(key):
		if sel.Active() {
			// Collapse to the edge the key moves toward, without moving further.
			start, end := sel.Range()
			if k.CharLeft.Match(key) || k.LineStart.Match(key) {
				sel.Collapse(start)
			} else {
				sel.Collapse(end)
			}
			b.SetCursor(sel.Cursor)
			return nil, true
		}
		cmd := b.Update(msg)
		sel.Collapse(b.Cursor())
		return cmd, true
	}

	if msg.Paste && msg.Type == tea.KeyRunes {
		// Bracketed paste is sanitized here, so the widget never turns newlines
		// into spaces.
		e.insert(sel, b, sanitize(string(msg.Runes), multiLine(b)))
		return nil, true
	}

	if editsText(msg, e.Keys, b) {
		if sel.Active() {
			// Typed or pasted text replaces the selection. It is inserted here
			// rather than forwarded, so it does not depend on the widget's focus.
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				text := string(msg.Runes)
				if msg.Type == tea.KeySpace && text == "" {
					text = " "
				}
				e.insert(sel, b, text)
				return nil, true
			}
			e.deleteSelection(sel, b)
			// Deletion keys remove only the selection when one exists.
			if isDeleteKey(msg, k) {
				return nil, true
			}
		}
		cmd := b.Update(msg)
		sel.Collapse(b.Cursor())
		return cmd, true
	}

	return nil, false
}

// Sync reconciles the selection after the widget handled a message the editor
// did not own. A selection whose moving end still matches the widget cursor is
// kept, so redraws and cursor blinks do not clear it. A cursor the widget moved
// elsewhere collapses the selection onto it.
func (e Editor) Sync(sel *Selection, b Backend) {
	if sel.Active() && b.Cursor() == sel.Cursor {
		return
	}
	sel.Collapse(b.Cursor())
}

// extend moves the moving end. The anchor stays put, so repeated Shift+Left
// keeps growing the same selection.
func (e Editor) extend(sel *Selection, b Backend, key string) {
	if !sel.Active() {
		sel.Collapse(b.Cursor())
	}
	runes := []rune(b.Text())
	pos := sel.Cursor
	k := e.Keys
	switch {
	case k.SelectCharLeft.Match(key):
		pos--
	case k.SelectCharRight.Match(key):
		pos++
	case k.SelectWordLeft.Match(key):
		pos = PrevWordBoundary(runes, pos)
	case k.SelectWordRight.Match(key):
		pos = NextWordBoundary(runes, pos)
	case k.SelectLineStart.Match(key):
		pos, _ = lineBounds(runes, pos)
	case k.SelectLineEnd.Match(key):
		_, pos = lineBounds(runes, pos)
	case k.SelectLineUp.Match(key):
		sel.Cursor = moveVertical(b, -1)
		return
	case k.SelectLineDown.Match(key):
		sel.Cursor = moveVertical(b, 1)
		return
	}
	sel.Cursor = clamp(pos, len(runes))
	b.SetCursor(sel.Cursor)
}

// lineBounds returns the [start, end) range of the logical line containing pos.
// Single-line text is one line, so the range is the whole text.
func lineBounds(runes []rune, pos int) (start, end int) {
	pos = clamp(pos, len(runes))
	start = pos
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	end = pos
	for end < len(runes) && runes[end] != '\n' {
		end++
	}
	return start, end
}

// deleteSelection removes the selected text and leaves the cursor at its start.
func (e Editor) deleteSelection(sel *Selection, b Backend) {
	start, end := sel.Range()
	runes := []rune(b.Text())
	end = clamp(end, len(runes))
	start = clamp(start, end)
	b.SetText(string(append(runes[:start:start], runes[end:]...)))
	sel.Collapse(start)
	b.SetCursor(start)
}

// InsertText replaces the selection (if any) with text at the cursor. Callers
// use it for keys the editor does not map itself, such as an explicit newline.
func (e Editor) InsertText(sel *Selection, b Backend, text string) {
	e.insert(sel, b, sanitize(text, multiLine(b)))
}

// insert replaces the selection (if any) with text at the cursor.
func (e Editor) insert(sel *Selection, b Backend, text string) {
	if sel.Active() {
		e.deleteSelection(sel, b)
	}
	runes := []rune(b.Text())
	pos := clamp(b.Cursor(), len(runes))
	ins := []rune(text)
	result := make([]rune, 0, len(runes)+len(ins))
	result = append(result, runes[:pos]...)
	result = append(result, ins...)
	result = append(result, runes[pos:]...)
	b.SetText(string(result))
	// The widget may rewrite the text (a textarea expands tabs), so measure
	// the cursor from the text that follows the insertion, not from len(ins).
	after := []rune(b.Text())
	b.SetCursor(len(after) - (len(runes) - pos))
	sel.Collapse(b.Cursor())
}

// editsText reports whether msg changes the text.
func editsText(msg tea.KeyMsg, k Keymap, b Backend) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete:
		return true
	case tea.KeyEnter:
		return multiLine(b)
	}
	return k.DeleteWord.Match(msg.String())
}

func isDeleteKey(msg tea.KeyMsg, k Keymap) bool {
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		return true
	}
	return k.DeleteWord.Match(msg.String())
}

// selectedText returns the text covered by the selection.
func selectedText(text string, sel Selection) string {
	start, end := sel.Range()
	runes := []rune(text)
	end = clamp(end, len(runes))
	start = clamp(start, end)
	return string(runes[start:end])
}

// sanitize makes pasted text fit the widget: a single line widget trims
// leading and trailing line breaks and turns each inner run of line breaks
// (\r\n, \r or \n) into one space.
func sanitize(text string, multiLine bool) string {
	if multiLine {
		// Clipboards on Windows use CRLF; the widget would show each as a
		// line break plus an extra blank line.
		return strings.ReplaceAll(text, "\r\n", "\n")
	}
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	return strings.Join(strings.FieldsFunc(text, func(r rune) bool { return r == '\n' }), " ")
}

func runeLen(s string) int { return len([]rune(s)) }
