//go:build e2e

package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestHarness_CanDriveBasicSearchFlow(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	seedRequests(
		t,
		st,
		col.ID,
		&domain.Request{ID: "req-1", Name: "Create User", Method: "POST", URL: "/users"},
	)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(
		t,
		m,
		tui.RequestsLoadedMsg(
			col.ID,
			[]*domain.Request{{ID: "req-1", Name: "Create User", Method: "POST", URL: "/users"}},
		),
	)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = callUpdate(
		t,
		m,
		tui.SearchResultsMsg(
			[]*search.SearchHit{
				{
					Request: &domain.Request{
						ID:     "req-1",
						Name:   "Create User",
						Method: "POST",
						URL:    "/users",
					},
				},
			},
		),
	)

	assertViewContains(t, m, "Search all requests")
	assertViewContains(t, m, "Create User")
}
