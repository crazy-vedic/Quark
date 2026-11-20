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

// Adding a second request with a name the collection already has is refused.
// The user is told why, in words, and the collection keeps its one request.
func TestE2E_AddRequestWithDuplicateNameExplainsTheRefusal(t *testing.T) {
	col := &domain.Collection{ID: "col-dup", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 120, 40)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	addRequestNamed := func(name string) tui.Model {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		require.Equal(t, tui.PromptAddRequest, m.PromptMode())
		for _, r := range name {
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		var cmd tea.Cmd
		m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		require.NotNil(t, cmd)
		return executeCmdUpdate(t, m, cmd)
	}

	m = addRequestNamed("Same")
	m = addRequestNamed("Same")

	require.NotNil(t, m.Err(), "the refused save must be reported")
	assert.Contains(t, m.Err().Error(), `a request named "Same" already exists in this collection`)

	reqs, err := st.ListRequests(context.Background(), col.ID)
	require.NoError(t, err)
	assert.Len(t, reqs, 1, "the duplicate must not be saved")
}
