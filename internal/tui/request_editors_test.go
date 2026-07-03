package tui_test

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestRequestEditors_HeaderKeyOpensHeaderEditor(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(filepath.Join(dir, "test.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "Create",
		Method:       "POST",
		URL:          "https://example.com",
		Headers:      "{}",
	}
	ctx := context.Background()
	require.NoError(t, st.SaveCollection(ctx, col))
	require.NoError(t, st.SaveRequest(ctx, req))

	m := tui.New(tui.Deps{
		Lister:   st,
		Reader:   st,
		Writer:   st,
		Executor: &exec.Executor{},
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   config.Default(t.TempDir()),
		Ctx:      ctx,
	})

	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.Equal(t, tui.HeadersField, m.ActiveField())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Len(t, m.HeaderPairs(), 1)
}
