package keybindings

import tea "github.com/charmbracelet/bubbletea"

// Keybindings holds every configurable action → key mapping.
// Fields are grouped by mode for readability.
type Keybindings struct {
	// Global (normal mode, available everywhere)
	Quit          string `toml:"quit"`
	Help          string `toml:"help"`
	Search        string `toml:"search"`
	FocusSidebar  string `toml:"focus_sidebar"`
	FocusRequest  string `toml:"focus_request"`
	FocusResponse string `toml:"focus_response"`
	PaneNext      string `toml:"pane_next"`
	PanePrev      string `toml:"pane_prev"`
	ImportCurl    string `toml:"import_curl"`

	// Sidebar
	SidebarDown       string `toml:"sidebar_down"`
	SidebarUp         string `toml:"sidebar_up"`
	SidebarExpand     string `toml:"sidebar_expand"`
	SidebarCollapse   string `toml:"sidebar_collapse"`
	SidebarAdd        string `toml:"sidebar_add"`
	SidebarDelete     string `toml:"sidebar_delete"`
	SidebarRename     string `toml:"sidebar_rename"`
	SidebarAddRequest string `toml:"sidebar_add_request"`

	// Request pane
	EditURL       string `toml:"edit_url"`
	MethodNext    string `toml:"method_next"`
	MethodPrev    string `toml:"method_prev"`
	SendRequest   string `toml:"send_request"`
	EditBody      string `toml:"edit_body"`
	EditHeaders   string `toml:"edit_headers"`
	RequestDelete string `toml:"request_delete"`
	EditAuth      string `toml:"edit_auth"`
	ScheduleRun   string `toml:"schedule_run"`
	EnvOpen       string `toml:"env_open"`
	ClientCerts   string `toml:"client_certificates"`
	EnvNext       string `toml:"env_next"`
	EnvPrev       string `toml:"env_prev"`

	// Env editor
	EnvSave            string `toml:"env_save"`
	EnvCancel          string `toml:"env_cancel"`
	EnvCreate          string `toml:"env_create"`
	EnvTabNext         string `toml:"env_tab_next"`
	EnvTabPrev         string `toml:"env_tab_prev"`
	EnvDown            string `toml:"env_down"`
	EnvUp              string `toml:"env_up"`
	EnvAdd             string `toml:"env_add"`
	EnvDelete          string `toml:"env_delete"`
	EnvEdit            string `toml:"env_edit"`
	EnvEditConfirm     string `toml:"env_edit_confirm"`
	EnvEditSwitchField string `toml:"env_edit_switch_field"`

	// Body editor
	BodySave    string `toml:"body_save"`
	BodyNewline string `toml:"body_newline"`
	BodyCancel  string `toml:"body_cancel"`

	// Header editor
	HeaderDown        string `toml:"header_down"`
	HeaderUp          string `toml:"header_up"`
	HeaderAdd         string `toml:"header_add"`
	HeaderDelete      string `toml:"header_delete"`
	HeaderEdit        string `toml:"header_edit"`
	HeaderSave        string `toml:"header_save"`
	HeaderCancel      string `toml:"header_cancel"`
	HeaderSwitchField string `toml:"header_switch_field"`

	// Auth editor
	AuthDown       string `toml:"auth_down"`
	AuthUp         string `toml:"auth_up"`
	AuthEdit       string `toml:"auth_edit"`
	AuthSave       string `toml:"auth_save"`
	AuthCancel     string `toml:"auth_cancel"`
	AuthOptionNext string `toml:"auth_option_next"`
	AuthOptionPrev string `toml:"auth_option_prev"`

	// Response pane
	ResponseDown  string `toml:"response_down"`
	ResponseUp    string `toml:"response_up"`
	ResponseRetry string `toml:"response_retry"`
	TabBody       string `toml:"tab_body"`
	TabHeaders    string `toml:"tab_headers"`
	TabRaw        string `toml:"tab_raw"`
	TabNext       string `toml:"tab_next"`
	TabPrev       string `toml:"tab_prev"`

	// Search modal
	SearchSelect string `toml:"search_select"`
	SearchDown   string `toml:"search_down"`
	SearchUp     string `toml:"search_up"`
	SearchCancel string `toml:"search_cancel"`

	// Help overlay
	HelpClose    string `toml:"help_close"`
	HelpDown     string `toml:"help_down"`
	HelpUp       string `toml:"help_up"`
	HelpEdit     string `toml:"help_edit"`
	HelpReset    string `toml:"help_reset"`
	HelpResetAll string `toml:"help_reset_all"`
	HelpUnbind   string `toml:"help_unbind"`

	// Import modal
	ImportConfirm string `toml:"import_confirm"`
	ImportParse   string `toml:"import_parse"`
	ImportCancel  string `toml:"import_cancel"`
}

// Resolver maps Bubble Tea key strings to action names per mode.
// It is immutable after construction; replace it (don't mutate) on rebinding.
type Resolver struct {
	global map[string]string // key → action

	// Normal mode, per-pane.
	sidebar  map[string]string
	request  map[string]string
	response map[string]string

	// Overlay modes.
	search     map[string]string
	help       map[string]string
	import_    map[string]string
	body       map[string]string
	header     map[string]string
	env        map[string]string
	collection map[string]string
	auth       map[string]string
}

// addAlias adds a key → action alias to a map, but only if the key is not
// already configured. This prevents a user binding from being overwritten by a
// fallback alias.
func addAlias(m map[string]string, key, action string) {
	if _, exists := m[key]; !exists {
		m[key] = action
	}
}

// NewResolver builds a Resolver from a Keybindings config.
func NewResolver(binds Keybindings) *Resolver {
	// Global keys — checked first in every mode.
	global := map[string]string{
		binds.Quit:          "quit",
		binds.Help:          "help",
		binds.Search:        ActionSearch,
		binds.FocusSidebar:  ActionFocusSidebar,
		binds.FocusRequest:  ActionFocusRequest,
		binds.FocusResponse: ActionFocusResponse,
		binds.PaneNext:      "pane_next",
		binds.PanePrev:      "pane_prev",
		binds.ImportCurl:    ActionImportCurl,
		binds.EnvOpen:       ActionEnvOpen,
		binds.ClientCerts:   ActionClientCerts,
		binds.EnvNext:       "env_next",
		binds.EnvPrev:       "env_prev",
	}

	// Sidebar
	sidebar := map[string]string{
		binds.SidebarDown:       "cursor_down",
		binds.SidebarUp:         "cursor_up",
		binds.SidebarExpand:     "expand",
		binds.SidebarCollapse:   "collapse",
		binds.SidebarAdd:        "add",
		binds.SidebarDelete:     "delete",
		binds.SidebarRename:     "rename",
		binds.SidebarAddRequest: "add_request",
	}
	// Arrow-key aliases mirror the hardcoded fallbacks in handleSidebarKey.
	addAlias(sidebar, "down", "cursor_down")
	addAlias(sidebar, "up", "cursor_up")
	addAlias(sidebar, "enter", "expand")
	addAlias(sidebar, "right", "expand")
	addAlias(sidebar, "left", "collapse")

	// Request pane
	request := map[string]string{
		binds.EditURL:       ActionEditURL,
		binds.MethodNext:    ActionMethodNext,
		binds.MethodPrev:    ActionMethodPrev,
		binds.SendRequest:   ActionSendRequest,
		binds.EditBody:      ActionEditBody,
		binds.EditHeaders:   ActionEditHeaders,
		binds.RequestDelete: ActionDeleteRequest,
		binds.EditAuth:      "edit_auth",
		binds.ScheduleRun:   ActionScheduleRun,
	}
	addAlias(request, "alt+enter", ActionSendRequest)

	// Response pane
	response := map[string]string{
		binds.ResponseDown:  "history_next",
		binds.ResponseUp:    "history_prev",
		binds.ResponseRetry: "retry_request",
		binds.TabBody:       "tab_body",
		binds.TabHeaders:    "tab_headers",
		binds.TabRaw:        "tab_raw",
		binds.TabNext:       "tab_next",
		binds.TabPrev:       "tab_prev",
	}
	addAlias(response, "down", "history_next")
	addAlias(response, "up", "history_prev")
	addAlias(response, "left", "tab_prev")
	addAlias(response, "right", "tab_next")

	// Search
	search := map[string]string{
		binds.SearchSelect: "select",
		binds.SearchDown:   ActionNavigateDown,
		binds.SearchUp:     ActionNavigateUp,
		binds.SearchCancel: ActionCancel,
	}

	// Help
	help := map[string]string{
		binds.HelpClose:    ActionClose,
		binds.HelpDown:     ActionNavigateDown,
		binds.HelpUp:       ActionNavigateUp,
		binds.HelpEdit:     "edit",
		binds.HelpReset:    "reset",
		binds.HelpResetAll: "reset_all",
	}
	addAlias(help, "down", ActionNavigateDown)
	addAlias(help, "up", ActionNavigateUp)

	// Import
	import_ := map[string]string{
		binds.ImportParse:   ActionImportParse,
		binds.ImportConfirm: ActionConfirm,
		binds.ImportCancel:  ActionCancel,
	}

	// Env editor
	env := map[string]string{
		binds.EnvSave:    "save",
		binds.EnvCancel:  ActionCancel,
		binds.EnvCreate:  "create_env",
		binds.EnvTabNext: "tab_next",
		binds.EnvTabPrev: "tab_prev",
		binds.EnvDown:    ActionNavigateDown,
		binds.EnvUp:      ActionNavigateUp,
		binds.EnvAdd:     "add",
		binds.EnvDelete:  "delete",
		binds.EnvEdit:    "edit",
	}
	addAlias(env, "down", ActionNavigateDown)
	addAlias(env, "up", ActionNavigateUp)
	addAlias(env, "left", "tab_prev")
	addAlias(env, "right", "tab_next")

	// Body editor
	body := map[string]string{
		binds.BodySave:    "save",
		binds.BodyNewline: "insert_newline",
		binds.BodyCancel:  ActionCancel,
	}
	addAlias(body, "enter", "save")

	// Header editor
	header := map[string]string{
		binds.HeaderDown:        ActionNavigateDown,
		binds.HeaderUp:          ActionNavigateUp,
		binds.HeaderAdd:         "add_pair",
		binds.HeaderDelete:      "delete_pair",
		binds.HeaderEdit:        "edit_pair",
		binds.HeaderSave:        "save",
		binds.HeaderCancel:      ActionCancel,
		binds.HeaderSwitchField: "switch_field",
	}
	addAlias(header, "down", ActionNavigateDown)
	addAlias(header, "up", ActionNavigateUp)
	addAlias(header, "enter", "save")
	addAlias(header, "ctrl+s", "save")

	// Collection prompt (cancel is the only binding; confirm is always Enter).
	collection := map[string]string{
		binds.ImportConfirm: ActionConfirm,
		binds.ImportCancel:  ActionCancel,
	}

	auth := map[string]string{
		binds.AuthDown:       ActionNavigateDown,
		binds.AuthUp:         ActionNavigateUp,
		binds.AuthEdit:       "edit",
		binds.AuthSave:       "save",
		binds.AuthCancel:     ActionCancel,
		binds.AuthOptionNext: "option_next",
		binds.AuthOptionPrev: "option_prev",
	}
	addAlias(auth, "down", ActionNavigateDown)
	addAlias(auth, "up", ActionNavigateUp)
	addAlias(auth, "left", "option_prev")
	addAlias(auth, "right", "option_next")
	addAlias(auth, "enter", "save")

	return &Resolver{
		global:     global,
		sidebar:    sidebar,
		request:    request,
		response:   response,
		search:     search,
		help:       help,
		import_:    import_,
		body:       body,
		header:     header,
		env:        env,
		collection: collection,
		auth:       auth,
	}
}

// Resolve returns the action name for a given key in a given mode.
// mode: 0=normal, 1=search, 2=help, 3=import, 4=body, 5=header, 6=env, 7=collection prompt, 8=auth
// pane: 0=sidebar, 1=request, 2=response (only used in normal mode)
func (r *Resolver) Resolve(mode, pane int, msg tea.KeyMsg) (string, bool) {
	key := msg.String()

	// Validate mode.
	if mode < 0 || mode > 8 {
		return "", false
	}

	// Overlay modes (search, help, import, body, header) check mode-specific first,
	// then global, so that e.g. tab in the header editor maps to switch_field rather
	// than pane_next.
	if mode != 0 {
		if action, ok := r.lookupOverlay(mode, key); ok {
			return action, true
		}
		if action, ok := r.global[key]; ok {
			return action, true
		}
		return "", false
	}

	// Normal mode: global keys checked first, then pane-specific.
	if action, ok := r.global[key]; ok {
		return action, true
	}

	switch pane {
	case 0:
		if action, ok := r.sidebar[key]; ok {
			return action, true
		}
	case 1:
		if action, ok := r.request[key]; ok {
			return action, true
		}
	case 2:
		if action, ok := r.response[key]; ok {
			return action, true
		}
	}

	return "", false
}

// lookupOverlay returns the action for an overlay mode (search, help, import, body, header, env, collection, auth).
func (r *Resolver) lookupOverlay(mode int, key string) (string, bool) {
	switch mode {
	case 1:
		if action, ok := r.search[key]; ok {
			return action, true
		}
	case 2:
		if action, ok := r.help[key]; ok {
			return action, true
		}
	case 3:
		if action, ok := r.import_[key]; ok {
			return action, true
		}
	case 4:
		if action, ok := r.body[key]; ok {
			return action, true
		}
	case 5:
		if action, ok := r.header[key]; ok {
			return action, true
		}
	case 6:
		if action, ok := r.env[key]; ok {
			return action, true
		}
	case 7:
		if action, ok := r.collection[key]; ok {
			return action, true
		}
	case 8:
		if action, ok := r.auth[key]; ok {
			return action, true
		}
	}
	return "", false
}

// Validate returns a map of duplicate keys: key → []actions.
// Checks (1) within each mode, and (2) global keys against normal-mode keys.
// Overlay modes are not checked against global because they take precedence.
// An empty map means no conflicts.
func Validate(binds Keybindings) map[string][]string {
	// binding is a (key, action) pair.
	type binding struct {
		key    string
		action string
	}

	global := []binding{
		{binds.Quit, "quit"}, {binds.Help, "help"}, {binds.Search, ActionSearch},
		{binds.FocusSidebar, ActionFocusSidebar}, {binds.FocusRequest, ActionFocusRequest},
		{binds.FocusResponse, ActionFocusResponse}, {binds.PaneNext, "pane_next"},
		{binds.PanePrev, "pane_prev"}, {binds.ImportCurl, ActionImportCurl},
		{binds.ClientCerts, ActionClientCerts},
	}
	modes := [][]binding{
		// normal mode panes
		{
			{binds.SidebarDown, "sidebar_down"},
			{binds.SidebarUp, "sidebar_up"},
			{binds.SidebarExpand, "sidebar_expand"},
			{binds.SidebarCollapse, "sidebar_collapse"},
			{binds.SidebarAdd, "sidebar_add"},
			{binds.SidebarDelete, "sidebar_delete"},
			{
				binds.SidebarRename,
				"sidebar_rename",
			},
			{binds.SidebarAddRequest, "sidebar_add_request"},
		},
		{{binds.EditURL, ActionEditURL}, {binds.MethodNext, ActionMethodNext},
			{binds.MethodPrev, ActionMethodPrev}, {binds.SendRequest, ActionSendRequest},
			{binds.EditBody, ActionEditBody}, {binds.EditHeaders, ActionEditHeaders},
			{binds.RequestDelete, ActionDeleteRequest},
			{binds.EditAuth, "edit_auth"}, {binds.ScheduleRun, ActionScheduleRun}},
		{{binds.ResponseDown, "response_down"}, {binds.ResponseUp, "response_up"},
			{binds.ResponseRetry, "response_retry"},
			{binds.TabBody, "tab_body"}, {binds.TabHeaders, "tab_headers"},
			{binds.TabRaw, "tab_raw"}, {binds.TabNext, "tab_next"},
			{binds.TabPrev, "tab_prev"}},
		// overlay modes
		{{binds.SearchSelect, "search_select"}, {binds.SearchDown, "search_down"},
			{binds.SearchUp, "search_up"}, {binds.SearchCancel, "search_cancel"}},
		{{binds.HelpClose, "help_close"}, {binds.HelpDown, "help_down"},
			{binds.HelpUp, "help_up"}, {binds.HelpEdit, "help_edit"},
			{binds.HelpReset, "help_reset"}, {binds.HelpResetAll, "help_reset_all"},
			{binds.HelpUnbind, "help_unbind"}},
		{{binds.ImportParse, ActionImportParse}, {binds.ImportConfirm, "import_confirm"},
			{binds.ImportCancel, ActionImportCancel}},
		{{binds.BodySave, "body_save"}, {binds.BodyNewline, "body_newline"},
			{binds.BodyCancel, "body_cancel"}},
		{{binds.HeaderDown, "header_down"}, {binds.HeaderUp, "header_up"},
			{binds.HeaderAdd, "header_add"}, {binds.HeaderDelete, "header_delete"},
			{binds.HeaderEdit, "header_edit"}, {binds.HeaderSave, "header_save"},
			{binds.HeaderCancel, "header_cancel"},
			{binds.HeaderSwitchField, "header_switch_field"}},
		{{binds.EnvSave, "env_save"}, {binds.EnvCancel, "env_cancel"},
			{binds.EnvCreate, "env_create"}, {binds.EnvTabNext, "env_tab_next"},
			{binds.EnvTabPrev, "env_tab_prev"}, {binds.EnvDown, "env_down"},
			{binds.EnvUp, "env_up"}, {binds.EnvAdd, "env_add"},
			{binds.EnvDelete, "env_delete"}, {binds.EnvEdit, "env_edit"}},
		{{binds.EnvCancel, "env_cancel"}, {binds.EnvEditConfirm, "env_edit_confirm"},
			{binds.EnvEditSwitchField, "env_edit_switch_field"}},
		{{binds.ImportConfirm, "import_confirm"}, {binds.ImportCancel, ActionImportCancel}},
		{{binds.AuthDown, "auth_down"}, {binds.AuthUp, "auth_up"},
			{binds.AuthEdit, "auth_edit"}, {binds.AuthSave, "auth_save"},
			{binds.AuthCancel, "auth_cancel"}, {binds.AuthOptionNext, "auth_option_next"},
			{binds.AuthOptionPrev, "auth_option_prev"}},
	}

	conflicts := make(map[string][]string)

	// Check within each mode.
	all := append([][]binding{global}, modes...)
	for _, mode := range all {
		byKey := make(map[string][]string)
		for _, b := range mode {
			if b.key == "" {
				continue
			}
			byKey[b.key] = append(byKey[b.key], b.action)
		}
		for key, actions := range byKey {
			if len(actions) > 1 {
				conflicts[key] = append(conflicts[key], actions...)
			}
		}
	}

	// Check global keys against normal-mode keys only (first 3 modes).
	// Overlay modes take precedence at runtime, so no conflict.
	for _, g := range global {
		if g.key == "" {
			continue
		}
		for _, mode := range modes[:3] {
			for _, b := range mode {
				if b.key == g.key {
					conflicts[b.key] = append(conflicts[b.key], g.action, b.action)
				}
			}
		}
	}

	return conflicts
}
