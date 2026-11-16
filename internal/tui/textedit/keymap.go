package textedit

import "github.com/crazy-vedic/quark/internal/keybindings"

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
	// Emacs-style deletes. They edit the text, so with a selection they remove
	// the selection, like Backspace and Delete do.
	DeleteCharForward, DeleteWordForward Binding
	DeleteToLineStart, DeleteToLineEnd   Binding

	// Find and viewer keys, used by the response viewer and its find field.
	FindOpen           Binding
	FindNext, FindPrev Binding
	ViewerFind         Binding
	ViewerCopy         Binding
	ViewerClose        Binding
	HistoryNext        Binding
	HistoryPrev        Binding
}

// DefaultKeymap returns the built-in bindings. Only strings that bubbletea
// reports are listed: alt+shift variants are omitted because bubbletea has no
// key for them.
func DefaultKeymap() Keymap {
	return Keymap{
		CharLeft:          Binding{"left"},
		CharRight:         Binding{"right"},
		WordLeft:          Binding{"ctrl+left", "alt+left"},
		WordRight:         Binding{"ctrl+right", "alt+right"},
		LineStart:         Binding{"home"},
		LineEnd:           Binding{"end"},
		SelectCharLeft:    Binding{"shift+left"},
		SelectCharRight:   Binding{"shift+right"},
		SelectWordLeft:    Binding{"ctrl+shift+left"},
		SelectWordRight:   Binding{"ctrl+shift+right"},
		SelectLineStart:   Binding{"shift+home"},
		SelectLineEnd:     Binding{"shift+end"},
		LineUp:            Binding{"up"},
		LineDown:          Binding{"down"},
		SelectLineUp:      Binding{"shift+up"},
		SelectLineDown:    Binding{"shift+down"},
		SelectAll:         Binding{"ctrl+a"},
		Copy:              Binding{"ctrl+c"},
		Cut:               Binding{"ctrl+x"},
		Paste:             Binding{"ctrl+v"},
		DeleteWord:        Binding{"ctrl+w", "alt+backspace"},
		DeleteCharForward: Binding{"ctrl+d"},
		DeleteWordForward: Binding{"alt+d"},
		DeleteToLineStart: Binding{"ctrl+u"},
		DeleteToLineEnd:   Binding{"ctrl+k"},

		FindOpen:    Binding{"ctrl+f", "cmd+f"},
		FindNext:    Binding{"enter"},
		FindPrev:    Binding{"shift+enter", "shift+return", "alt+enter"},
		ViewerFind:  Binding{"f"},
		ViewerCopy:  Binding{"c"},
		ViewerClose: Binding{"esc", "tab", "shift+tab"},
		HistoryNext: Binding{"shift+down", "shift+pgdown", "shift+pagedown", "alt+pgdown"},
		HistoryPrev: Binding{"shift+up", "shift+pgup", "shift+pageup", "alt+pgup"},
	}
}

// KeymapFromConfig starts from DefaultKeymap and replaces each action whose
// config value is set and differs from the config default. An unset action,
// or one left at its config default, keeps the editor's own bindings.
func KeymapFromConfig(cfg keybindings.Keybindings) Keymap {
	k := DefaultKeymap()
	defs := keybindings.DefaultKeybindings()
	override := func(dst *Binding, value, def string) {
		if value != "" && value != def {
			*dst = Binding{value}
		}
	}
	override(&k.SelectCharLeft, cfg.SelectCharLeft, defs.SelectCharLeft)
	override(&k.SelectCharRight, cfg.SelectCharRight, defs.SelectCharRight)
	override(&k.SelectWordLeft, cfg.SelectWordLeft, defs.SelectWordLeft)
	override(&k.SelectWordRight, cfg.SelectWordRight, defs.SelectWordRight)
	override(&k.SelectLineStart, cfg.SelectLineStart, defs.SelectLineStart)
	override(&k.SelectLineEnd, cfg.SelectLineEnd, defs.SelectLineEnd)
	override(&k.SelectAll, cfg.SelectAll, defs.SelectAll)
	override(&k.Copy, cfg.Copy, defs.Copy)
	override(&k.Cut, cfg.Cut, defs.Cut)
	override(&k.Paste, cfg.Paste, defs.Paste)
	override(&k.CharLeft, cfg.CharLeft, defs.CharLeft)
	override(&k.CharRight, cfg.CharRight, defs.CharRight)
	override(&k.WordLeft, cfg.WordLeft, defs.WordLeft)
	override(&k.WordRight, cfg.WordRight, defs.WordRight)
	override(&k.LineStart, cfg.LineStart, defs.LineStart)
	override(&k.LineEnd, cfg.LineEnd, defs.LineEnd)
	override(&k.LineUp, cfg.LineUp, defs.LineUp)
	override(&k.LineDown, cfg.LineDown, defs.LineDown)
	override(&k.SelectLineUp, cfg.SelectLineUp, defs.SelectLineUp)
	override(&k.SelectLineDown, cfg.SelectLineDown, defs.SelectLineDown)
	override(&k.DeleteWord, cfg.DeleteWord, defs.DeleteWord)
	override(&k.FindOpen, cfg.FindOpen, defs.FindOpen)
	override(&k.FindNext, cfg.FindNext, defs.FindNext)
	override(&k.FindPrev, cfg.FindPrev, defs.FindPrev)
	override(&k.ViewerFind, cfg.ViewerFind, defs.ViewerFind)
	override(&k.ViewerCopy, cfg.ViewerCopy, defs.ViewerCopy)
	override(&k.ViewerClose, cfg.ViewerClose, defs.ViewerClose)
	override(&k.HistoryNext, cfg.HistoryNext, defs.HistoryNext)
	override(&k.HistoryPrev, cfg.HistoryPrev, defs.HistoryPrev)
	return k
}
