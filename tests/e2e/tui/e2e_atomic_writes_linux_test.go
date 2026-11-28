//go:build e2e && linux

package tui_test

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tuitest"
	"github.com/crazy-vedic/quark/tests/e2e/writewatch"
)

// The TUI-only savers (last selected request, theme chosen from the command
// palette) must replace config.toml atomically, never edit it in place. See
// tests/e2e/cli/atomic_writes_linux_test.go for the CLI savers.
func TestE2E_TUIConfigWritesAreAtomic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(
		configPath, []byte("[ui]\ntheme = \"dark\"\neditor = \"nano\"\n"), 0o600,
	))

	col := &domain.Collection{ID: "atomic-col", Name: "API"}
	req := &domain.Request{
		ID: "atomic-req", Name: "Get widgets", CollectionID: col.ID,
		Method: "GET", URL: "https://example.test/widgets",
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)
	m := tuitest.NewModelWithConfigDir(t, st, &mockExecutor{}, config.Default(dir), dir)
	m = resize(t, m, 120, 36)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))

	expectAtomic := func(what string, act func()) {
		t.Helper()
		w := writewatch.Start(t, dir)
		act()
		report := w.Stop()
		assert.Empty(t, report.InPlace, "%s wrote files in place: %s", what, report)
		assert.Equal(t, []string{"config.toml"}, report.Replaced, "%s: %s", what, report)
	}

	expectAtomic("selecting a request", func() { m, _ = m.SelectRequest(req) })
	expectAtomic("choosing a theme", func() {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		for _, key := range "> theme: light" {
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		}
		var saveCmd tea.Cmd
		m, saveCmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		// The write runs in a tea.Cmd; the runtime delivers its result to Update.
		m = callUpdate(t, m, saveCmd())
	})

	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "light", saved.UI.Theme)
	assert.Equal(t, req.ID, saved.UI.LastRequestID)
	assert.Equal(t, "nano", saved.UI.Editor, "other settings must survive")
}
