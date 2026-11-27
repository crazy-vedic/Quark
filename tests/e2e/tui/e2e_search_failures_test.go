//go:build e2e

package tui_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestE2E_SearchTellsUserWhenACollectionWasSkipped(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "search.db")
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	good := &domain.Collection{ID: "col-good", Name: "Good"}
	broken := &domain.Collection{ID: "col-broken", Name: "Broken"}
	require.NoError(t, st.SaveCollection(ctx, good))
	require.NoError(t, st.SaveCollection(ctx, broken))
	seedRequests(t, st, good.ID,
		&domain.Request{ID: "req-ok", Name: "Healthy Request", Method: "GET", URL: "/ok"})
	seedRequests(t, st, broken.ID,
		&domain.Request{ID: "req-bad", Name: "Unreadable Request", Method: "GET", URL: "/bad"})

	// Make the broken collection's rows unscannable: sort_order is an integer column.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(
		`UPDATE requests SET sort_order = 'not-a-number' WHERE collection_id = ?`,
		broken.ID,
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{good, broken}))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	require.Equal(t, tui.SearchMode, m.Mode())

	// Typing starts a real search against the store. Each keystroke cancels the
	// previous search, so only the command from the last keystroke is run.
	var cmd tea.Cmd
	for _, r := range "request" {
		m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	require.NotNil(t, cmd)
	if batch, ok := runCmd(t, cmd).(tea.BatchMsg); ok {
		for _, part := range batch {
			if part != nil {
				m = executeCmdUpdate(t, m, part)
			}
		}
	} else {
		t.Fatal("expected the keystroke to dispatch a batch containing the search")
	}

	require.Len(t, m.SearchResults(), 1, "hits from the readable collection are kept")
	assert.Equal(t, "Healthy Request", m.SearchResults()[0].Request.Name)
	assert.Equal(t, "search skipped 1 collection(s); results may be incomplete", m.StatusErr())
}

// The status warning is also reachable from a pre-built result message.
func TestE2E_SearchResultsWithFailuresSetStatus(t *testing.T) {
	m := newE2EModel(t, setupStore(t), &mockExecutor{})
	m = callUpdate(t, m, tui.SearchResultsMsgWithFailures(nil, []search.CollectionFailure{
		{CollectionID: "a", Err: assert.AnError},
		{CollectionID: "b", Err: assert.AnError},
	}))
	assert.Equal(t, "search skipped 2 collection(s); results may be incomplete", m.StatusErr())
}
