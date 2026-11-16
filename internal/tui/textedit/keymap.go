package textedit

// Binding is the set of key strings (as reported by tea.KeyMsg.String()) for
// one action.
type Binding []string

// Match reports whether key is one of the binding's strings.
func (b Binding) Match(key string) bool {
	for _, k := range b {
		if k == key {
			return true
		}
	}
	return false
}

// Keymap maps editor actions to key strings. It is plain data so that config
// overrides can replace any entry.
type Keymap struct {
	CharLeft, CharRight  Binding
	WordLeft, WordRight  Binding
	LineStart, LineEnd   Binding
	SelectCharLeft       Binding
	SelectCharRight      Binding
	SelectWordLeft       Binding
	SelectWordRight      Binding
	SelectLineStart      Binding
	SelectLineEnd        Binding
	LineUp, LineDown     Binding
	SelectLineUp         Binding
	SelectLineDown       Binding
	SelectAll, Copy, Cut Binding
	Paste, DeleteWord    Binding
}

// DefaultKeymap returns the built-in bindings. Only strings that bubbletea
// reports are listed: alt+shift variants are omitted because bubbletea has no
// key for them.
func DefaultKeymap() Keymap {
	return Keymap{
		CharLeft:        Binding{"left"},
		CharRight:       Binding{"right"},
		WordLeft:        Binding{"ctrl+left", "alt+left"},
		WordRight:       Binding{"ctrl+right", "alt+right"},
		LineStart:       Binding{"home"},
		LineEnd:         Binding{"end"},
		SelectCharLeft:  Binding{"shift+left"},
		SelectCharRight: Binding{"shift+right"},
		SelectWordLeft:  Binding{"ctrl+shift+left"},
		SelectWordRight: Binding{"ctrl+shift+right"},
		SelectLineStart: Binding{"shift+home"},
		SelectLineEnd:   Binding{"shift+end"},
		LineUp:          Binding{"up"},
		LineDown:        Binding{"down"},
		SelectLineUp:    Binding{"shift+up"},
		SelectLineDown:  Binding{"shift+down"},
		SelectAll:       Binding{"ctrl+a"},
		Copy:            Binding{"ctrl+c"},
		Cut:             Binding{"ctrl+x"},
		Paste:           Binding{"ctrl+v"},
		DeleteWord:      Binding{"ctrl+w", "alt+backspace"},
	}
}
