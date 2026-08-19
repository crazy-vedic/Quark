// Package keybindings implements dynamic keybinding resolution and capture.
package keybindings

// KeyEnter is the canonical key name for the Enter/Return key.
const KeyEnter = "enter"

// DefaultKeybindings returns the built-in keybindings that match the current
// hardcoded behaviour in the TUI.
func DefaultKeybindings() Keybindings {
	return Keybindings{
		// Global (normal mode, always available)
		Quit:          "q",
		Help:          "?",
		Search:        "/",
		FocusSidebar:  "1",
		FocusRequest:  "2",
		FocusResponse: "3",
		PaneNext:      "tab",
		PanePrev:      KeyShiftTab,
		ImportCurl:    "I",

		// Sidebar
		SidebarDown:       "j",
		SidebarUp:         "k",
		SidebarExpand:     "l",
		SidebarCollapse:   "h",
		SidebarAdd:        "A",
		SidebarDelete:     "d",
		SidebarRename:     "r",
		SidebarAddRequest: "a",

		// Request pane
		EditURL:     "u",
		MethodNext:  "m",
		MethodPrev:  "M",
		SendRequest: "s",
		EditBody:    "b",
		EditHeaders: "h",
		EditAuth:    "a",
		ScheduleRun: "S",
		EnvOpen:     "e",
		ClientCerts: "C",
		EnvNext:     "shift+right",
		EnvPrev:     "shift+left",

		// Env editor
		EnvSave:            "s",
		EnvCancel:          "esc",
		EnvCreate:          "A",
		EnvTabNext:         "tab",
		EnvTabPrev:         KeyShiftTab,
		EnvDown:            "j",
		EnvUp:              "k",
		EnvAdd:             "a",
		EnvDelete:          "d",
		EnvEdit:            "e",
		EnvEditConfirm:     KeyEnter,
		EnvEditSwitchField: "tab",

		// Body editor
		BodySave:    KeyEnter,
		BodyNewline: KeyCtrlJ,
		BodyCancel:  "esc",

		// Header editor
		HeaderDown:        "j",
		HeaderUp:          "k",
		HeaderAdd:         "a",
		HeaderDelete:      "d",
		HeaderEdit:        "e",
		HeaderSave:        KeyEnter,
		HeaderCancel:      "esc",
		HeaderSwitchField: "ctrl+e",

		// Auth editor
		AuthDown:       "j",
		AuthUp:         "k",
		AuthEdit:       "e",
		AuthSave:       KeyEnter,
		AuthCancel:     "esc",
		AuthOptionNext: "right",
		AuthOptionPrev: "left",

		// Response pane
		ResponseDown:  "j",
		ResponseUp:    "k",
		ResponseRetry: "R",
		TabBody:       "b",
		TabHeaders:    "h",
		TabRaw:        "r",
		TabNext:       "right",
		TabPrev:       "left",

		// Search modal
		SearchSelect: KeyEnter,
		SearchDown:   "down",
		SearchUp:     "up",
		SearchCancel: "esc",

		// Help overlay
		HelpClose:    "esc",
		HelpDown:     "j",
		HelpUp:       "k",
		HelpEdit:     KeyEnter,
		HelpReset:    "r",
		HelpResetAll: "R",
		HelpUnbind:   "backspace",

		// Import modal
		ImportConfirm: KeyEnter,
		ImportParse:   "ctrl+s",
		ImportCancel:  "esc",
	}
}
