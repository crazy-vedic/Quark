package keybindings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetAction(t *testing.T) {
	binds := DefaultKeybindings()
	assert.Equal(t, "q", GetAction(binds, "quit"))
	assert.Equal(t, "j", GetAction(binds, "sidebar_down"))
	assert.Equal(t, "", GetAction(binds, "nonexistent"))
}

func TestSetAction(t *testing.T) {
	binds := DefaultKeybindings()
	assert.NoError(t, SetAction(&binds, "quit", "Q"))
	assert.Equal(t, "Q", binds.Quit)

	assert.Error(t, SetAction(&binds, "nonexistent", "x"))
}

func TestListEntries(t *testing.T) {
	binds := DefaultKeybindings()
	entries := ListEntries(binds)

	assert.NotEmpty(t, entries)

	// First entry should be quit.
	assert.Equal(t, "quit", entries[0].Action)
	assert.Equal(t, "q", entries[0].Key)
	assert.Equal(t, "Global", entries[0].Group)

	// Sidebar entries should follow.
	var sidebarFound bool
	for _, e := range entries {
		if e.Group == "Sidebar" && e.Action == "sidebar_down" {
			sidebarFound = true
			assert.Equal(t, "j", e.Key)
		}
	}
	assert.True(t, sidebarFound, "sidebar_down entry should exist")
}

func TestRecordBinding(t *testing.T) {
	binds := DefaultKeybindings()

	// Change quit to Q.
	newBinds, err := RecordBinding(binds, "quit", "Q")
	assert.NoError(t, err)
	assert.Equal(t, "Q", newBinds.Quit)

	// Revert.
	binds = newBinds

	// Try to create a conflict: bind quit to j (already sidebar_down).
	_, err = RecordBinding(binds, "quit", "j")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
	// The message is shown to the user: it must read as a sentence, not as
	// Go's slice or map syntax.
	assert.Contains(t, err.Error(), "j is bound to")
	assert.NotContains(t, err.Error(), "[")
	assert.NotContains(t, err.Error(), "map")

	// Original bindings should be unchanged.
	assert.Equal(t, "Q", binds.Quit)
}

func TestRecordBinding_Unbind(t *testing.T) {
	binds := DefaultKeybindings()

	newBinds, err := RecordBinding(binds, "quit", "")
	assert.NoError(t, err)
	assert.Equal(t, "", newBinds.Quit)

	// Validation should still catch conflicts if we rebind something else to j.
	_, err = RecordBinding(newBinds, "sidebar_down", "j")
	assert.NoError(t, err) // j is already sidebar_down, so no conflict.
}

func TestDefaultWordSelectBindings(t *testing.T) {
	// bubbletea reports ctrl+shift+left/right, not alt+shift variants, so the
	// config defaults must use the combinations the editor actually receives.
	defs := DefaultKeybindings()
	assert.Equal(t, "ctrl+shift+left", defs.SelectWordLeft)
	assert.Equal(t, "ctrl+shift+right", defs.SelectWordRight)
}

func TestListEntries_IncludesTextSelectionActions(t *testing.T) {
	entries := ListEntries(DefaultKeybindings())
	want := map[string]string{
		"select_char_left": "shift+left", "select_char_right": "shift+right",
		"select_word_left": "ctrl+shift+left", "select_word_right": "ctrl+shift+right",
		"select_line_start": "shift+home", "select_line_end": "shift+end",
		"select_all": "ctrl+a", "copy": "ctrl+c", "cut": "ctrl+x",
	}
	for action, key := range want {
		var found *Entry
		for i := range entries {
			if entries[i].Action == action {
				found = &entries[i]
				break
			}
		}
		if assert.NotNil(t, found, "%s should be listed", action) {
			assert.Equal(t, key, found.Key, action)
			assert.Equal(t, "Text Selection", found.Group, action)
		}
	}
}
