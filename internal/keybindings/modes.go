package keybindings

// Mode selects which binding table Resolve consults.
type Mode int

// Resolver modes. ModeNormal uses the global table plus the focused pane's
// table; every other mode is an overlay whose table is checked before the
// global keys.
const (
	ModeNormal Mode = iota
	ModeSearch
	ModeHelp
	ModeImport
	ModeBody
	ModeHeader
	ModeEnv
	ModeCollectionPrompt
	ModeAuth
	ModeCertificate
)

// Pane selects the pane-specific table in ModeNormal.
type Pane int

// Panes consulted by Resolve in ModeNormal.
const (
	PaneSidebar Pane = iota
	PaneRequest
	PaneResponse
)
