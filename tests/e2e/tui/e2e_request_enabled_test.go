//go:build e2e

package tui_test

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// Disabling a request from the sidebar persists to the store, marks it in the
// sidebar and title, refuses scheduling while off, and re-enabling restores it.
func TestE2E_RequestEnableToggleFromSidebarPersistsAndBlocksSchedule(t *testing.T) {
	ctx := context.Background()
	col := &domain.Collection{ID: uuid.NewString(), Name: "Toggles"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Ping",
		Method:       "GET",
		URL:          "https://ping.example",
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithFocus(tui.SidebarPane)

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, `Request "Ping" disabled`, m.StatusSuccess())
	assertViewContains(t, m, "Ping (off)")
	assertViewContains(t, m, "(disabled)")

	persisted, err := st.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.True(t, persisted.Disabled, "disabled state must be written to the store")

	m = callUpdate(t, m.WithActiveRequest(req).WithFocus(tui.RequestPane), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "scheduling a disabled request must not open the prompt")
	assert.Contains(t, m.StatusErr(), "request is disabled")

	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, `Request "Ping" enabled`, m.StatusSuccess())
	assertViewNotContains(t, m, "(off)")

	persisted, err = st.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.False(t, persisted.Disabled, "re-enabling must clear the stored flag")
}

// The command palette entry reaches the same toggle as the keybinding.
func TestE2E_RequestEnableToggleFromCommandPalette(t *testing.T) {
	ctx := context.Background()
	col := &domain.Collection{ID: uuid.NewString(), Name: "Palette"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           uuid.NewString(),
		CollectionID: col.ID,
		Name:         "Palette request",
		Method:       "GET",
		URL:          "https://palette.example",
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{}).WithActiveRequest(req).WithFocus(tui.RequestPane)
	m, cmd := choosePaletteCommand(t, m, "enable or disable request")
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, `Request "Palette request" disabled`, m.StatusSuccess())

	persisted, err := st.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.True(t, persisted.Disabled)
}
