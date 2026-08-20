package keybindings

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

// --- Resolve: default bindings ---

func TestResolver_Default_Global(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	tests := []struct {
		key    string
		mode   int
		pane   int
		want   string
		wantOk bool
	}{
		{"q", 0, 0, "quit", true},
		{"?", 0, 0, "help", true},
		{"/", 0, 0, "search", true},
		{"1", 0, 0, "focus_sidebar", true},
		{"2", 0, 1, "focus_request", true},
		{"3", 0, 2, "focus_response", true},
		{"tab", 0, 0, "pane_next", true},
		{"shift+tab", 0, 0, "pane_prev", true},
		{"x", 0, 0, "", false},
	}

	for _, tc := range tests {
		var msg tea.KeyMsg
		switch tc.key {
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)}
		}
		got, ok := r.Resolve(tc.mode, tc.pane, msg)
		assert.Equal(t, tc.wantOk, ok, "key=%q mode=%d pane=%d", tc.key, tc.mode, tc.pane)
		assert.Equal(t, tc.want, got, "key=%q mode=%d pane=%d", tc.key, tc.mode, tc.pane)
	}
}

func TestResolver_Default_Sidebar(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	tests := []struct {
		key  string
		want string
	}{
		{"j", "cursor_down"},
		{"k", "cursor_up"},
		{"l", "expand"},
		{"h", "collapse"},
		{"a", "add_request"},
		{"A", "add"},
		{"D", "delete"},
		{"r", "rename"},
	}

	for _, tc := range tests {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)}
		got, ok := r.Resolve(0, 0, msg)
		assert.True(t, ok, "key=%q", tc.key)
		assert.Equal(t, tc.want, got, "key=%q", tc.key)
	}

	msg := tea.KeyMsg{Type: tea.KeyRight}
	got, ok := r.Resolve(0, 0, msg)
	assert.True(t, ok, "key=right")
	assert.Equal(t, "expand", got, "key=right")
}

func TestResolver_Default_RequestPane(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	tests := []struct {
		key  string
		want string
	}{
		{"u", "edit_url"},
		{"m", "method_next"},
		{"s", "send_request"},
		{"S", "schedule_run"},
		{"b", "edit_body"},
		{"h", "edit_headers"},
		{"d", "delete_request"},
	}

	for _, tc := range tests {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)}
		got, ok := r.Resolve(0, 1, msg)
		assert.True(t, ok, "key=%q", tc.key)
		assert.Equal(t, tc.want, got, "key=%q", tc.key)
	}
}

func TestResolver_Default_ResponsePane(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	tests := []struct {
		key  string
		want string
	}{
		{"j", "history_next"},
		{"k", "history_prev"},
		{"R", "retry_request"},
		{"b", "tab_body"},
		{"h", "tab_headers"},
		{"r", "tab_raw"},
	}

	for _, tc := range tests {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)}
		got, ok := r.Resolve(0, 2, msg)
		assert.True(t, ok, "key=%q", tc.key)
		assert.Equal(t, tc.want, got, "key=%q", tc.key)
	}
}

func TestResolver_Default_SearchMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	got, ok := r.Resolve(1, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "select", got)

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	_, ok = r.Resolve(1, 0, msg)
	assert.False(t, ok, "unmapped key in search mode should not resolve")

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	got, ok = r.Resolve(1, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "cancel", got)
}

func TestResolver_Default_HelpMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	got, ok := r.Resolve(2, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "close", got)

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	got, ok = r.Resolve(2, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "navigate_down", got)

	msg = tea.KeyMsg{Type: tea.KeyEnter}
	got, ok = r.Resolve(2, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "edit", got)

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	got, ok = r.Resolve(2, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "quit", got)
}

func TestResolver_Default_EnvMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	got, ok := r.Resolve(6, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "add", got)

	msg = tea.KeyMsg{Type: tea.KeyTab}
	got, ok = r.Resolve(6, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "tab_next", got)

	msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	got, ok = r.Resolve(6, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "tab_prev", got)

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	got, ok = r.Resolve(6, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "cancel", got)

}

func TestResolver_Default_CollectionPromptMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	got, ok := r.Resolve(7, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "confirm", got)

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	got, ok = r.Resolve(7, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "cancel", got)
}

func TestResolver_Default_ImportMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	got, ok := r.Resolve(3, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "confirm", got)

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	got, ok = r.Resolve(3, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "cancel", got)
}

func TestResolver_Default_BodyMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	got, ok := r.Resolve(4, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "save", got)

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	got, ok = r.Resolve(4, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "cancel", got)

	msg = tea.KeyMsg{Type: tea.KeyCtrlJ}
	got, ok = r.Resolve(4, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "insert_newline", got)
}

func TestResolver_Default_HeaderMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	got, ok := r.Resolve(5, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "save", got)

	msg = tea.KeyMsg{Type: tea.KeyEsc}
	got, ok = r.Resolve(5, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "cancel", got)

	msg = tea.KeyMsg{Type: tea.KeyCtrlE}
	got, ok = r.Resolve(5, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "switch_field", got)
}

// --- Resolve: custom bindings ---

func TestResolver_Custom_Bindings(t *testing.T) {
	binds := Keybindings{
		Quit:          "Q",
		Help:          "H",
		Search:        "S",
		FocusSidebar:  "!",
		FocusRequest:  "@",
		FocusResponse: "#",
		PaneNext:      "ctrl+n",
		PanePrev:      "ctrl+p",

		SidebarDown:       "J",
		SidebarUp:         "K",
		SidebarExpand:     "L",
		SidebarCollapse:   "H",
		SidebarAdd:        "A",
		SidebarDelete:     "D",
		SidebarRename:     "X",
		SidebarAddRequest: "R",

		EditURL:     "U",
		MethodNext:  "N",
		MethodPrev:  "P",
		SendRequest: "ctrl+s",
		EditBody:    "B",
		EditHeaders: "E",

		ResponseDown:  "ctrl+j",
		ResponseUp:    "ctrl+k",
		ResponseRetry: "ctrl+r",
		TabBody:       "B",
		TabHeaders:    "H",
		TabRaw:        "T",
		TabNext:       "ctrl+right",
		TabPrev:       "ctrl+left",

		SearchSelect: "enter",
		SearchDown:   "ctrl+j",
		SearchUp:     "ctrl+k",
		SearchCancel: "esc",

		ImportConfirm: "enter",
		ImportCancel:  "esc",

		BodySave:    "ctrl+enter",
		BodyNewline: "ctrl+j",
		BodyCancel:  "esc",

		HeaderSave:        "ctrl+enter",
		HeaderCancel:      "esc",
		HeaderSwitchField: "tab",
		HeaderDown:        "ctrl+j",
		HeaderUp:          "ctrl+k",
		HeaderAdd:         "ctrl+a",
		HeaderDelete:      "ctrl+d",
		HeaderEdit:        "ctrl+e",
	}

	r := NewResolver(binds)

	// Custom quit
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}}
	got, ok := r.Resolve(0, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "quit", got)

	// Old quit key should NOT work
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, ok = r.Resolve(0, 0, msg)
	assert.False(t, ok, "old quit key should not work after rebinding")

	// Custom send_request
	msg = tea.KeyMsg{Type: tea.KeyCtrlS}
	got, ok = r.Resolve(0, 1, msg)
	assert.True(t, ok)
	assert.Equal(t, "send_request", got)

	// Custom focus keys
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}}
	got, ok = r.Resolve(0, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "focus_sidebar", got)
}

// --- Per-mode key reuse ---

func TestResolver_PerMode_KeyReuse(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	// "h" means different things in different contexts.
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}

	// sidebar: collapse
	got, ok := r.Resolve(0, 0, msg)
	assert.True(t, ok)
	assert.Equal(t, "collapse", got)

	// request pane: edit_headers
	got, ok = r.Resolve(0, 1, msg)
	assert.True(t, ok)
	assert.Equal(t, "edit_headers", got)

	// response pane: tab_headers
	got, ok = r.Resolve(0, 2, msg)
	assert.True(t, ok)
	assert.Equal(t, "tab_headers", got)

	// search mode: no action (cancel is esc)
	_, ok = r.Resolve(1, 0, msg)
	assert.False(t, ok)
}

// --- Validate ---

func TestValidate_NoConflicts(t *testing.T) {
	binds := DefaultKeybindings()
	conflicts := Validate(binds)
	assert.Empty(t, conflicts, "default bindings should have no conflicts")
}

func TestValidate_WithinModeConflict(t *testing.T) {
	binds := DefaultKeybindings()
	binds.SidebarDown = "j"
	binds.SidebarUp = "j" // conflict: same key for two actions
	conflicts := Validate(binds)
	assert.Contains(t, conflicts, "j")
	assert.ElementsMatch(t, []string{"sidebar_down", "sidebar_up"}, conflicts["j"])
}

func TestValidate_CrossModeConflict(t *testing.T) {
	binds := DefaultKeybindings()
	binds.Quit = "j" // global "j" conflicts with sidebar_down
	conflicts := Validate(binds)
	assert.Contains(t, conflicts, "j")
	assert.Contains(t, conflicts["j"], "quit")
	assert.Contains(t, conflicts["j"], "sidebar_down")
}

func TestValidate_MultipleConflicts(t *testing.T) {
	binds := DefaultKeybindings()
	binds.Quit = "j"      // conflicts with sidebar_down and response_down
	binds.Help = "j"      // conflicts with sidebar_down, response_down, and quit
	binds.SidebarUp = "k" // no conflict
	conflicts := Validate(binds)
	assert.Contains(t, conflicts, "j")
	assert.Contains(t, conflicts["j"], "quit")
	assert.Contains(t, conflicts["j"], "help")
	assert.Contains(t, conflicts["j"], "sidebar_down")
	assert.Contains(t, conflicts["j"], "response_down")
}

func TestValidate_EmptyKeyIgnored(t *testing.T) {
	binds := DefaultKeybindings()
	binds.Quit = "" // empty means unbound
	conflicts := Validate(binds)
	assert.Empty(t, conflicts)
}

// --- Edge cases ---

func TestResolver_Nil(t *testing.T) {
	var r *Resolver
	assert.Nil(t, r)
	// Resolve on nil would panic; callers must guard with nil check.
}

func TestResolver_UnknownMode(t *testing.T) {
	r := NewResolver(DefaultKeybindings())
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, ok := r.Resolve(99, 0, msg)
	assert.False(t, ok)
}

func TestResolver_UnknownPane(t *testing.T) {
	r := NewResolver(DefaultKeybindings())
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	_, ok := r.Resolve(0, 99, msg)
	assert.False(t, ok)
}

// TestResolver_ArrowAliases ensures arrow-key aliases that mirror the hardcoded
// fallback behaviour are present in the resolver. This is a regression test for
// the bug where arrow keys became no-ops after the resolver was introduced.
func TestResolver_ArrowAliases(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	// Sidebar arrow keys
	msg := tea.KeyMsg{Type: tea.KeyDown}
	got, ok := r.Resolve(0, 0, msg)
	assert.True(t, ok, "down arrow in sidebar")
	assert.Equal(t, "cursor_down", got)

	msg = tea.KeyMsg{Type: tea.KeyUp}
	got, ok = r.Resolve(0, 0, msg)
	assert.True(t, ok, "up arrow in sidebar")
	assert.Equal(t, "cursor_up", got)

	msg = tea.KeyMsg{Type: tea.KeyEnter}
	got, ok = r.Resolve(0, 0, msg)
	assert.True(t, ok, "enter in sidebar")
	assert.Equal(t, "expand", got)

	msg = tea.KeyMsg{Type: tea.KeyLeft}
	got, ok = r.Resolve(0, 0, msg)
	assert.True(t, ok, "left in sidebar")
	assert.Equal(t, "collapse", got)

	// Response pane arrow keys
	msg = tea.KeyMsg{Type: tea.KeyLeft}
	got, ok = r.Resolve(0, 2, msg)
	assert.True(t, ok, "left in response pane")
	assert.Equal(t, "tab_prev", got)

	msg = tea.KeyMsg{Type: tea.KeyRight}
	got, ok = r.Resolve(0, 2, msg)
	assert.True(t, ok, "right in response pane")
	assert.Equal(t, "tab_next", got)

	msg = tea.KeyMsg{Type: tea.KeyDown}
	got, ok = r.Resolve(0, 2, msg)
	assert.True(t, ok, "down in response pane")
	assert.Equal(t, "history_next", got)

	msg = tea.KeyMsg{Type: tea.KeyUp}
	got, ok = r.Resolve(0, 2, msg)
	assert.True(t, ok, "up in response pane")
	assert.Equal(t, "history_prev", got)

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}}
	got, ok = r.Resolve(0, 2, msg)
	assert.True(t, ok, "uppercase retry in response pane")
	assert.Equal(t, "retry_request", got)

	// Search mode aliases
	msg = tea.KeyMsg{Type: tea.KeyDown}
	got, ok = r.Resolve(1, 0, msg)
	assert.True(t, ok, "down in search mode")
	assert.Equal(t, "navigate_down", got)

	msg = tea.KeyMsg{Type: tea.KeyUp}
	got, ok = r.Resolve(1, 0, msg)
	assert.True(t, ok, "up in search mode")
	assert.Equal(t, "navigate_up", got)

	msg = tea.KeyMsg{Type: tea.KeyEnter}
	got, ok = r.Resolve(4, 0, msg)
	assert.True(t, ok, "enter in body editor")
	assert.Equal(t, "save", got)

	// Header editor aliases
	msg = tea.KeyMsg{Type: tea.KeyDown}
	got, ok = r.Resolve(5, 0, msg)
	assert.True(t, ok, "down in header editor")
	assert.Equal(t, "navigate_down", got)

	msg = tea.KeyMsg{Type: tea.KeyUp}
	got, ok = r.Resolve(5, 0, msg)
	assert.True(t, ok, "up in header editor")
	assert.Equal(t, "navigate_up", got)

	msg = tea.KeyMsg{Type: tea.KeyCtrlS}
	got, ok = r.Resolve(5, 0, msg)
	assert.True(t, ok, "ctrl+s in header editor")
	assert.Equal(t, "save", got)
}

func TestResolver_RemovedAliases_DoNotResolveByDefault(t *testing.T) {
	r := NewResolver(DefaultKeybindings())

	_, ok := r.Resolve(1, 0, tea.KeyMsg{Type: tea.KeyCtrlJ})
	assert.False(t, ok, "ctrl+j should not be a hardcoded search alias")

	_, ok = r.Resolve(1, 0, tea.KeyMsg{Type: tea.KeyCtrlK})
	assert.False(t, ok, "ctrl+k should not be a hardcoded search alias")

	_, ok = r.Resolve(0, 1, tea.KeyMsg{Type: tea.KeyCtrlJ})
	assert.False(t, ok, "ctrl+j should not be a hardcoded request-pane alias")
}
