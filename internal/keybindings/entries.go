package keybindings

import (
	"fmt"
	"reflect"
)

// Entry is a single configurable keybinding for display/editing.
type Entry struct {
	Action string // toml tag name, e.g. "quit"
	Key    string // current key, e.g. "q"
	Group  string // "Global", "Sidebar", ...
}

// groups defines the display order and membership of every configurable action.
// Order here is the order shown in the help editor.
var groups = []struct {
	Name    string
	Actions []string
}{
	{"Global", []string{
		"quit", "help", ActionSearch,
		ActionFocusSidebar, ActionFocusRequest, ActionFocusResponse,
		"pane_next", "pane_prev",
	}},
	{"Sidebar", []string{
		"sidebar_down", "sidebar_up", "sidebar_expand", "sidebar_collapse",
		"sidebar_add_request", "sidebar_add", "sidebar_delete", "sidebar_rename",
	}},
	{"Env Editor", []string{
		"env_save", "env_cancel", "env_create",
		"env_tab_next", "env_tab_prev",
		"env_down", "env_up",
		"env_add", "env_delete", "env_edit",
		"env_edit_confirm", "env_edit_switch_field",
	}},
	{"Request", []string{
		ActionEditURL, ActionMethodNext, ActionMethodPrev,
		ActionSendRequest, ActionScheduleRun, ActionEditBody, ActionEditHeaders, "edit_auth",
	}},
	{"Body Editor", []string{
		"body_save", "body_newline", "body_cancel",
	}},
	{"Header Editor", []string{
		"header_down", "header_up", "header_add", "header_delete",
		"header_edit", "header_save", "header_cancel", "header_switch_field",
	}},
	{"Auth Editor", []string{
		"auth_down", "auth_up", "auth_edit", "auth_save",
		"auth_cancel", "auth_option_next", "auth_option_prev",
	}},
	{"Response", []string{
		"response_down", "response_up", "response_retry",
		"tab_body", "tab_headers", "tab_raw", "tab_next", "tab_prev",
	}},
	{"Search", []string{
		"search_select", "search_down", "search_up", "search_cancel",
	}},
	{"Help", []string{
		"help_close", "help_down", "help_up",
		"help_edit", "help_reset", "help_reset_all", "help_unbind",
	}},
	{"Import", []string{
		"import_confirm", ActionImportCancel,
	}},
}

// ListEntries returns all keybindings as a flat, ordered list of entries.
func ListEntries(binds Keybindings) []Entry {
	var entries []Entry
	v := reflect.ValueOf(binds)
	t := v.Type()

	for _, g := range groups {
		for _, action := range g.Actions {
			key := GetAction(binds, action)
			entries = append(entries, Entry{
				Action: action,
				Key:    key,
				Group:  g.Name,
			})
		}
	}

	// Populate from struct fields for any actions not in the hardcoded list.
	// This ensures new fields are visible even if groups isn't updated.
	for i := 0; i < v.NumField(); i++ {
		action := t.Field(i).Tag.Get("toml")
		if action == "" {
			continue
		}
		// Skip if already in entries.
		found := false
		for _, e := range entries {
			if e.Action == action {
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, Entry{
				Action: action,
				Key:    v.Field(i).String(),
				Group:  "Other",
			})
		}
	}

	return entries
}

// GetAction returns the key string for a given action name.
// Returns "" if the action is unknown.
func GetAction(binds Keybindings, action string) string {
	v := reflect.ValueOf(binds)
	for i := 0; i < v.NumField(); i++ {
		tag := v.Type().Field(i).Tag.Get("toml")
		if tag == action {
			return v.Field(i).String()
		}
	}
	return ""
}

// SetAction sets the key for a given action name.
func SetAction(binds *Keybindings, action, key string) error {
	v := reflect.ValueOf(binds).Elem()
	for i := 0; i < v.NumField(); i++ {
		tag := v.Type().Field(i).Tag.Get("toml")
		if tag == action {
			v.Field(i).SetString(key)
			return nil
		}
	}
	return fmt.Errorf("unknown action: %s", action)
}

// RecordBinding clones binds, sets action to key, validates, and returns the clone.
// If validation fails, the original bindings are returned along with a conflict error.
func RecordBinding(binds Keybindings, action, key string) (Keybindings, error) {
	clone := binds
	if err := SetAction(&clone, action, key); err != nil {
		return binds, err
	}
	conflicts := Validate(clone)
	if len(conflicts) > 0 {
		return binds, fmt.Errorf("conflict: %s", formatConflicts(conflicts))
	}
	return clone, nil
}

func formatConflicts(c map[string][]string) string {
	var parts []string
	for key, actions := range c {
		parts = append(parts, fmt.Sprintf("%s is bound to %s", key, actions))
	}
	return fmt.Sprintf("%v", parts)
}
