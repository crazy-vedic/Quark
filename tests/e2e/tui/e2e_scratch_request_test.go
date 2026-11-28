//go:build e2e

package tui_test

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

func typeText(t *testing.T, m tui.Model, text string) tui.Model {
	t.Helper()
	for _, r := range text {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// The scratch section sits after the last collection. 'a' on it creates a
// request that belongs to no collection.
func TestE2E_ScratchRequest_AddFromScratchSectionHasNoCollection(t *testing.T) {
	col := &domain.Collection{ID: "col-api", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = m.WithColCursor(1).WithFocus(tui.SidebarPane)
	assertViewContains(t, m, "Scratch")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.Equal(t, tui.PromptAddRequest, m.PromptMode())
	m = typeText(t, m, "Loose Request")

	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, tui.PromptNone, m.PromptMode())

	scratch, err := st.ListRequests(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, scratch, 1)
	assert.Equal(t, "Loose Request", scratch[0].Name)
	assert.Empty(t, scratch[0].CollectionID, "a scratch request belongs to no collection")

	inCollection, err := st.ListRequests(context.Background(), col.ID)
	require.NoError(t, err)
	assert.Empty(t, inCollection)
}

func TestE2E_ScratchRequest_MoveToCollectionThenBackToNone(t *testing.T) {
	api := &domain.Collection{ID: "col-api", Name: "API"}
	other := &domain.Collection{ID: "col-other", Name: "Other"}
	st := setupStore(t, api, other)
	ping := &domain.Request{ID: "req-ping", Name: "Ping", Method: "GET", URL: "https://example.com/ping"}
	seedRequests(t, st, api.ID, ping)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{api, other}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(api.ID, []*domain.Request{ping}))
	m = m.WithColCursor(0).WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	require.Equal(t, 0, m.ReqCursor(), "j must select the first request under API")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	require.Equal(t, tui.PromptMoveRequest, m.PromptMode())
	assertViewContains(t, m, "Move Request")

	// An unknown collection name is refused and nothing moves.
	m = typeText(t, m, "Missing")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assertViewContains(t, m, `No collection named "Missing"`)
	moved, err := st.GetRequest(context.Background(), ping.ID)
	require.NoError(t, err)
	assert.Equal(t, api.ID, moved.CollectionID)

	// Retype the name of an existing collection; the move runs and the sidebar
	// reports it.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = typeText(t, m, "Other")
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, `Moved "Ping" to "Other"`, m.StatusSuccess())
	moved, err = st.GetRequest(context.Background(), ping.ID)
	require.NoError(t, err)
	assert.Equal(t, other.ID, moved.CollectionID)
	assert.Equal(t, tui.PromptNone, m.PromptMode())

	// Blank target moves the request to the scratch section.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	m = executeCmdUpdate(t, m, cmd)
	assert.Equal(t, `Moved "Ping" to no collection`, m.StatusSuccess())
	moved, err = st.GetRequest(context.Background(), ping.ID)
	require.NoError(t, err)
	assert.Empty(t, moved.CollectionID)
}

func TestE2E_ScratchRequest_MoveNeedsSavedRequest(t *testing.T) {
	api := &domain.Collection{ID: "col-api", Name: "API"}
	st := setupStore(t, api)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{api}))
	m = m.WithColCursor(0).WithFocus(tui.SidebarPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	assert.NotEqual(t, tui.PromptMoveRequest, m.PromptMode(), "a collection row is not a request")
	assertViewContains(t, m, "Select a saved request first")
}
