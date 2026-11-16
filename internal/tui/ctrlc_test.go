package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/tui"
)

// Ctrl+C must never quit the app, in any mode or field, with or without a selection.
func TestCtrlCNeverQuits(t *testing.T) {
	base := newModel(defaultConfig()).WithFocus(tui.RequestPane)
	cases := map[string]tui.Model{
		"normal":       base,
		"url field":    base.WithActiveField(tui.URLField),
		"body field":   bodyModel(t, "some body"),
		"header field": base.WithActiveField(tui.HeadersField),
		"auth field":   base.WithActiveField(tui.AuthField),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			if cmd != nil {
				_, quit := cmd().(tea.QuitMsg)
				assert.False(t, quit, "Ctrl+C quit the app in %s", name)
			}
		})
	}
}
