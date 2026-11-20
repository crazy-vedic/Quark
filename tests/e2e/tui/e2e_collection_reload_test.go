//go:build e2e

package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

func collectionIndex(m tui.Model, id string) int {
	for i, col := range m.Collections() {
		if col.ID == id {
			return i
		}
	}
	return -1
}

// Adding a collection reloads the list. The user's selection must stay on the
// collection they were on, even though its position in the list moved.
func TestE2E_CollectionReloadKeepsSelectedCollection(t *testing.T) {
	alpha := &domain.Collection{ID: "col-alpha", Name: "Alpha"}
	bravo := &domain.Collection{ID: "col-bravo", Name: "Bravo"}
	charlie := &domain.Collection{ID: "col-charlie", Name: "Charlie"}
	st := setupStore(t, alpha, bravo, charlie)
	seedRequests(t, st, bravo.ID,
		&domain.Request{ID: "req-bravo", Name: "Bravo Request", Method: "GET", URL: "/bravo"})

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 100, 30)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{alpha, bravo, charlie}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, collectionIndex(m, bravo.ID), m.ColCursor(), "Bravo is selected")

	// A new collection sorts before Bravo, so Bravo moves down one row.
	aardvark := &domain.Collection{ID: "col-aardvark", Name: "Aardvark"}
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m,
		tui.CollectionsLoadedMsg([]*domain.Collection{alpha, bravo, charlie, aardvark}))

	want := collectionIndex(m, bravo.ID)
	require.GreaterOrEqual(t, want, 0)
	assert.Equal(t, want, m.ColCursor(), "selection follows Bravo, not row 0")
	assert.Equal(t, bravo.ID, m.Collections()[m.ColCursor()].ID)

	// The selected collection's requests are refreshed.
	require.NotNil(t, cmd, "reload refreshes the selected collection's requests")
	m = executeCmdUpdate(t, m, cmd)
	require.Len(t, m.Requests(), 1)
	assert.Equal(t, "Bravo Request", m.Requests()[0].Name)
}

// With no request reader there is nothing to refresh, but the selection is
// still kept.
func TestE2E_CollectionReloadKeepsSelectionWithoutReader(t *testing.T) {
	m := tui.New(tui.Deps{Config: config.Default("")})
	m = resize(t, m, 100, 30)
	alpha := &domain.Collection{ID: "col-alpha", Name: "Alpha"}
	bravo := &domain.Collection{ID: "col-bravo", Name: "Bravo"}
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{alpha, bravo}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, collectionIndex(m, bravo.ID), m.ColCursor())

	aardvark := &domain.Collection{ID: "col-aardvark", Name: "Aardvark"}
	m, cmd := callUpdateWithCmd(t, m,
		tui.CollectionsLoadedMsg([]*domain.Collection{alpha, bravo, aardvark}))
	assert.Equal(t, collectionIndex(m, bravo.ID), m.ColCursor())
	assert.Nil(t, cmd)
}
