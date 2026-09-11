package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
)

type startupRequestReader struct {
	requests map[string][]*domain.Request
}

func (r startupRequestReader) GetRequest(_ context.Context, id string) (*domain.Request, error) {
	for _, requests := range r.requests {
		for _, req := range requests {
			if req.ID == id {
				return req, nil
			}
		}
	}
	return nil, errors.New("request not found")
}

func (r startupRequestReader) ListRequests(_ context.Context, collectionID string) ([]*domain.Request, error) {
	return r.requests[collectionID], nil
}

func runStartupCmd(t *testing.T, m Model, cmd func() tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

func TestStartupRestoresLastRequest(t *testing.T) {
	first := &domain.Collection{ID: "first", Name: "First"}
	second := &domain.Collection{ID: "second", Name: "Second"}
	last := &domain.Request{ID: "last", CollectionID: second.ID, Name: "Last", Method: "GET", URL: "https://last.example"}
	reader := startupRequestReader{requests: map[string][]*domain.Request{second.ID: {last}}}
	cfg := config.Default(t.TempDir())
	cfg.UI.LastRequestID = last.ID
	m := New(Deps{Reader: reader, Config: cfg, ConfigDir: filepath.Dir(cfg.Logging.File)})

	updated, cmd := m.Update(collectionsLoadedMsg{collections: []*domain.Collection{first, second}})
	m = updated.(Model)
	updated, cmd = m.Update(cmd())
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if m.activeRequest == nil || m.activeRequest.ID != last.ID {
		t.Fatalf("active request = %v, want %q", m.activeRequest, last.ID)
	}
	if m.colCursor != 1 || m.reqCursor != 0 {
		t.Fatalf("sidebar selection = (%d, %d), want (1, 0)", m.colCursor, m.reqCursor)
	}
}

func TestStartupSelectsFirstRequestWhenNoRequestWasPersisted(t *testing.T) {
	col := &domain.Collection{ID: "col", Name: "Collection"}
	first := &domain.Request{ID: "first", CollectionID: col.ID, Name: "First", Method: "GET", URL: "https://first.example"}
	second := &domain.Request{ID: "second", CollectionID: col.ID, Name: "Second", Method: "GET", URL: "https://second.example"}
	reader := startupRequestReader{requests: map[string][]*domain.Request{col.ID: {first, second}}}
	m := New(Deps{Reader: reader, Config: config.Default(t.TempDir())})

	updated, cmd := m.Update(collectionsLoadedMsg{collections: []*domain.Collection{col}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if m.activeRequest == nil || m.activeRequest.ID != first.ID {
		t.Fatalf("active request = %v, want %q", m.activeRequest, first.ID)
	}
}

func TestRequestPaneShowsGettingStartedMessageWithoutRequests(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.width, m.height = 100, 30
	if got := m.viewRequestPane(100, 30); !strings.Contains(got, "Get started") {
		t.Fatalf("request pane = %q, want getting-started guidance", got)
	}
}
