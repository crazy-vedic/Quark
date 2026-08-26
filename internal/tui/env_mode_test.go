package tui_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
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
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

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

func TestUpdate_EnvModal_InheritedTabsAreReadOnlyAndEditCopiesToChildDefault(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	assert.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	root := &domain.Collection{ID: "root", Name: "Root"}
	parent := &domain.Collection{ID: "parent", ParentID: root.ID, Name: "Parent"}
	child := &domain.Collection{ID: "child", ParentID: parent.ID, Name: "Child"}
	assert.NoError(t, st.SaveCollection(ctx, root))
	assert.NoError(t, st.SaveCollection(ctx, parent))
	assert.NoError(t, st.SaveCollection(ctx, child))
	rootDefault, err := st.GetEnvironmentByName(ctx, root.ID, "default")
	assert.NoError(t, err)
	rootDefault.SetVars(map[string]string{"shared": "inherited-secret"})
	assert.NoError(t, st.SaveEnvironment(ctx, rootDefault))

	m := newModel(defaultConfig()).WithEnvReader(st).WithEnvWriter(st)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{root, parent, child}))
	m = m.WithColCursor(2).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	view := m.View()
	assert.Contains(t, view, "Root/default")
	assert.Contains(t, view, "Root/Parent/default")
	assert.Contains(t, view, "Root/Parent/Child/default")
	assert.Contains(t, view, "read-only")

	// Global add is rejected through the status line.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Contains(t, m.StatusErr(), "read-only")

	// Root default is the first tab after Global; editing copies without saving.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 1, m.EnvEditorTabIdx())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.True(t, m.EnvEditorEditing())
	vars := m.EnvEditorVars()
	assert.Len(t, vars, 1)
	assert.Equal(t, "shared", vars[0].Key)
	assert.False(t, vars[0].Saved)
	childDefault, err := st.GetEnvironmentByName(ctx, child.ID, "default")
	assert.NoError(t, err)
	assert.NotContains(t, childDefault.Vars(), "shared", "copy must remain unsaved")

	// If the child default already owns the key, editing inherited data is a no-op.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	childDefault.SetVars(map[string]string{"shared": "local-secret"})
	assert.NoError(t, st.SaveEnvironment(ctx, childDefault))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	beforeTab := m.EnvEditorTabIdx()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.Equal(t, beforeTab, m.EnvEditorTabIdx())
	assert.False(t, m.EnvEditorEditing())
	assert.Equal(t, `Key "shared" already exists in env "Root/Parent/Child/default"`, m.StatusErr())
	assert.NotContains(t, m.StatusErr(), "local-secret")
	assert.NotContains(t, m.StatusErr(), "inherited-secret")
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

func TestUpdate_EnvModal_TabsCycleAndCreateFromGlobal(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "Global"}
	global.SetVars(map[string]string{})
	first := &domain.Environment{ID: "env-1", CollectionID: col1, Name: "default"}
	first.SetVars(map[string]string{})
	second := &domain.Environment{ID: "env-2", CollectionID: col1, Name: "dev"}
	second.SetVars(map[string]string{})
	reader := &fakeEnvReader{
		global: global,
		envs: map[string]*domain.Environment{
			global.ID: global,
			first.ID:  first,
			second.ID: second,
		},
		byCol: map[string][]*domain.Environment{
			col1: {first, second},
		},
	}

	m := newModel(defaultConfig()).WithEnvReader(reader)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{{ID: col1, Name: "Alpha"}}))
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	assert.Equal(t, 0, m.EnvEditorTabIdx())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 1, m.EnvEditorTabIdx())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 2, m.EnvEditorTabIdx())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 0, m.EnvEditorTabIdx())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, 2, m.EnvEditorTabIdx())

	// Uppercase A must also work while the Global tab is selected.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode())
	assert.Equal(t, tui.PromptAddEnv, m.PromptMode())
}

func TestUpdate_EnvModal_UnsavedDraftSurvivesTabNavigation(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "Global", Data: `{}`}
	childDefault := &domain.Environment{ID: "default", CollectionID: col1, Name: "default", Data: `{}`}
	reader := &fakeEnvReader{
		global: global,
		envs:   map[string]*domain.Environment{global.ID: global, childDefault.ID: childDefault},
		byCol:  map[string][]*domain.Environment{col1: {childDefault}},
	}
	m := newModel(defaultConfig()).WithEnvReader(reader)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{{ID: col1, Name: "Alpha"}}))
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "draft-key" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Len(t, m.EnvEditorVars(), 1)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	require.Len(t, m.EnvEditorVars(), 1)
	assert.Equal(t, "draft-key", m.EnvEditorVars()[0].Key)
	assert.False(t, m.EnvEditorVars()[0].Saved)
}
