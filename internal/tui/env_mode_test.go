package tui_test

import (
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestView_EnvModal_UsesConfiguredBindings(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "Global"}
	global.SetVars(map[string]string{})
	collectionEnv := &domain.Environment{ID: "env-1", CollectionID: col1, Name: "default"}
	collectionEnv.SetVars(map[string]string{})

	reader := &fakeEnvReader{
		global: global,
		envs: map[string]*domain.Environment{
			"global": global,
			"env-1":  collectionEnv,
		},
		byCol: map[string][]*domain.Environment{
			col1: {collectionEnv},
		},
	}

	cfg := defaultConfig()
	cfg.Keybindings.EnvTabPrev = "u"
	cfg.Keybindings.EnvTabNext = "o"
	cfg.Keybindings.EnvUp = "p"
	cfg.Keybindings.EnvDown = "n"
	cfg.Keybindings.EnvAdd = "z"
	cfg.Keybindings.EnvCreate = "G"
	cfg.Keybindings.EnvEdit = "i"
	cfg.Keybindings.EnvDelete = "D"
	cfg.Keybindings.EnvSave = "v"
	cfg.Keybindings.EnvCancel = "x"
	cfg.Keybindings.EnvEditSwitchField = "w"
	cfg.Keybindings.EnvEditConfirm = "c"

	m := newModel(cfg).WithEnvReader(reader)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{{ID: col1, Name: "Alpha"}}))
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	view := m.View()
	assert.Contains(t, view, "Press [z] to add")
	assert.Contains(t, view, "[u/o] tabs")
	assert.Contains(t, view, "[p/n] nav")
	assert.Contains(t, view, "[z] add var")
	assert.Contains(t, view, "[G] new env")
	assert.Contains(t, view, "[i] edit")
	assert.Contains(t, view, "[D]")
	assert.Contains(t, view, "delete")
	assert.Contains(t, view, "[v] save")
	assert.Contains(t, view, "[x] close")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	view = m.View()
	assert.Contains(t, view, "[w] switch")
	assert.Contains(t, view, "[c] confirm")
	assert.Contains(t, view, "[x] cancel")
}

func TestView_EnvModal_ScrollsLongVariableLists(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "Global"}
	global.SetVars(map[string]string{})

	vars := make(map[string]string)
	for i := 1; i <= 8; i++ {
		vars["KEY_"+strconv.Itoa(i)] = "value-" + strconv.Itoa(i)
	}
	collectionEnv := &domain.Environment{ID: "env-1", CollectionID: col1, Name: "default"}
	collectionEnv.SetVars(vars)

	reader := &fakeEnvReader{
		global: global,
		envs: map[string]*domain.Environment{
			"global": global,
			"env-1":  collectionEnv,
		},
		byCol: map[string][]*domain.Environment{
			col1: {collectionEnv},
		},
	}

	m := newModel(defaultConfig()).WithEnvReader(reader)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 20})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{{ID: col1, Name: "Alpha"}}))
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})

	first := m.View()
	assert.Contains(t, first, "KEY_1")
	assert.Contains(t, first, "KEY_3")
	assert.NotContains(t, first, "KEY_8")
	assert.Contains(t, first, "↓ more below")

	for i := 0; i < 4; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}

	scrolled := m.View()
	assert.NotContains(t, scrolled, "KEY_1")
	assert.Contains(t, scrolled, "KEY_5")
	assert.Contains(t, scrolled, "↑ more above")
}
