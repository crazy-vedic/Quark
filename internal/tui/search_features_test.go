package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
)

type recordingAllSearcher struct {
	query         string
	collectionIDs []string
	hits          []*search.SearchHit
}

func (s *recordingAllSearcher) Search(context.Context, string, string) (*search.SearchResult, error) {
	return &search.SearchResult{}, nil
}

func (s *recordingAllSearcher) SearchAll(_ context.Context, query string, collectionIDs []string) (*search.SearchResult, error) {
	s.query = query
	s.collectionIDs = append([]string(nil), collectionIDs...)
	return &search.SearchResult{Hits: s.hits}, nil
}

func TestOpenSearchDispatchesOptionalEmptyQueryAndPopulatesResults(t *testing.T) {
	searcher := &recordingAllSearcher{hits: []*search.SearchHit{{
		Request: &domain.Request{Name: "List users"},
	}}}
	m := New(Deps{
		Config:   config.Default(t.TempDir()),
		Searcher: searcher,
	})
	m.collections = []*domain.Collection{
		{ID: "billing", Name: "Billing"},
		{ID: "accounts", Name: "Accounts"},
	}

	updated, _ := m.openSearch()
	m = updated
	require.Equal(t, searchMode, m.mode)

	_, cmd := m.dispatchSearch("")
	msg := cmd()
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(Model)
	require.Equal(t, "", searcher.query)
	require.Equal(t, []string{"billing", "accounts"}, searcher.collectionIDs)
	require.Len(t, m.searchResults, 1)
}

func TestHelpSearchFiltersKeybindingsLive(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.mode = helpMode

	updated, _ := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	require.True(t, m.helpSearch)

	for _, r := range "header_switch" {
		updated, _ = m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	entries := filterKeybindingEntries(keybindings.ListEntries(m.cfg.Keybindings), m.searchInput.Value())
	require.NotEmpty(t, entries)
	for _, entry := range entries {
		require.Equal(t, "Header Editor", entry.Group)
	}
}
