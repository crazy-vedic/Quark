package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestDebugHeaderPairs(t *testing.T) {
	dir := t.TempDir()
	st, _ := store.New(filepath.Join(dir, "test.db"), store.WithCacheSize(100))
	defer st.Close()
	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	req := &domain.Request{
		ID:      uuid.New().String(),
		Name:    "Create",
		Method:  "POST",
		URL:     "https://example.com",
		Headers: "{}",
	}
	req.CollectionID = col.ID
	ctx := context.Background()
	if err := st.SaveCollection(ctx, col); err != nil {
		t.Fatalf("save collection: %v", err)
	}
	if err := st.SaveRequest(ctx, req); err != nil {
		t.Fatalf("save request: %v", err)
	}

	m := New(Deps{
		Lister:   st,
		Reader:   st,
		Writer:   st,
		Executor: &exec.Executor{},
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   config.Default(t.TempDir()),
		Ctx:      ctx,
	})

	m = updateM(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateM(m, CollectionsLoadedMsg([]*domain.Collection{col}))
	m = updateM(m, RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(requestPane)

	m = updateM(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	_ = updateM(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
}

func updateM(m Model, msg tea.Msg) Model {
	newM, _ := m.Update(msg)
	model, ok := newM.(Model)
	if !ok {
		panic("Update did not return Model")
	}
	return model
}
