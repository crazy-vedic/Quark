package tui

import tea "github.com/charmbracelet/bubbletea"

// configSavedMsg reports the result of a config.toml write run by saveConfigCmd.
// done runs on the Update goroutine. It applies the change on success, or
// rolls back and shows the error on failure.
type configSavedMsg struct {
	err  error
	done func(m Model, err error) (Model, tea.Cmd)
}

// saveConfigCmd runs write off the Update goroutine, so the UI does not block
// on disk I/O. Set m.configSaving before returning it, and check configBusy
// first. Only one config write may be pending: each one is built from the
// state at the time, so a second write could overwrite the first.
func saveConfigCmd(write func() error, done func(Model, error) (Model, tea.Cmd)) tea.Cmd {
	return func() tea.Msg {
		return configSavedMsg{err: write(), done: done}
	}
}

// configBusy reports whether a config write is still pending. When it is, the
// status bar says so and the caller must not start another write.
func (m Model) configBusy() (Model, bool) {
	if !m.configSaving {
		return m, false
	}
	return m.status("warn", "Still saving config.toml, try again"), true
}
