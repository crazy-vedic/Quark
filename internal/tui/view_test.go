package tui_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestView_SearchModal_LongResultsStayWithinViewportAndKeepTitleVisible(t *testing.T) {
	m := newModel(defaultConfig()).
		WithMode(tui.SearchMode).
		WithSearchInputValue("notif")
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 16})
	m = callUpdate(t, m, tui.SearchResultsMsg([]*search.SearchHit{
		{
			Collection: &domain.Collection{Name: "Notifications"},
			Request: &domain.Request{
				ID:     "req-1",
				Name:   "Create Notification With A Very Long Human Readable Name",
				Method: "POST",
				URL: "https://api.example.com/v1/notifications?channel=email&priority=high" +
					"&recipient=user-42&include=metadata",
			},
		},
		{
			Collection: &domain.Collection{Name: "Inventory"},
			Request: &domain.Request{
				ID:     "req-2",
				Name:   "Delete Item With An Extremely Long Name That Should Never Wrap In The Modal",
				Method: "DELETE",
				URL:    "https://api.example.com/v1/items/12345?expand=history&include=all-fields",
			},
		},
	}))

	view := m.View()
	assert.Contains(t, view, "Search all requests")
	assert.LessOrEqual(
		t,
		lipgloss.Height(view),
		m.Height(),
		"search modal should stay within the terminal viewport",
	)
}

func TestView_SearchModal_ScrollsLongResults(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.SearchMode)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 14})

	var hits []*search.SearchHit
	for i := 1; i <= 8; i++ {
		hits = append(hits, &search.SearchHit{
			Request: &domain.Request{
				ID:     "r" + strconv.Itoa(i),
				Name:   "Result " + strconv.Itoa(i),
				Method: "GET",
			},
			Score: float64(10 - i),
		})
	}
	m = callUpdate(t, m, tui.SearchResultsMsg(hits))

	first := m.View()
	assert.Contains(t, first, "Result 1")
	assert.Contains(t, first, "Result 3")
	assert.NotContains(t, first, "Result 4")
	assert.Contains(t, first, "↓ more below")

	for i := 0; i < 4; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}

	scrolled := m.View()
	assert.NotContains(t, scrolled, "Result 1")
	assert.Contains(t, scrolled, "Result 5")
	assert.Contains(t, scrolled, "↑ more above")
	assert.Contains(t, scrolled, "↓ more below")
}

func TestView_SearchModal_ShowsCollectionRequestAndURL(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.SearchMode)
	m = m.WithCollections([]*domain.Collection{
		{ID: "col-1", Name: "Billing"},
	})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("invoice")})
	m = callUpdate(t, m, tui.SearchResultsMsg([]*search.SearchHit{
		{
			Request: &domain.Request{
				ID:           "req-1",
				CollectionID: "col-1",
				Name:         "Create Invoice",
				Method:       "POST",
				URL:          "/v1/invoices/create",
			},
		},
	}))

	view := m.View()
	assert.Contains(t, view, "Billing/Create ")
	assert.Contains(t, view, "/v1/invoices/create")
	assert.Contains(t, view, "(")
	assert.Contains(t, view, ")")
}

// --- Pane switching ---

func TestView_ResponseHistoryPopup_OnlyShowsWhileBrowsingOlderRuns(t *testing.T) {
	now := time.Now()
	executions := []*domain.Execution{
		{
			ID:           "newest",
			RequestID:    "req-1",
			StatusCode:   200,
			CompletedAt:  now,
			ResponseBody: `{"label":"current"}`,
		},
		{
			ID:           "older",
			RequestID:    "req-1",
			StatusCode:   500,
			CompletedAt:  now.Add(-time.Hour),
			ResponseBody: `{"label":"older"}`,
		},
	}

	m := newModel(defaultConfig()).
		WithFocus(tui.ResponsePane).
		WithExecutions(executions).
		WithExecCursor(0)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	view := m.View()
	assert.NotContains(t, view, "Execution History")
	assert.NotContains(t, view, "Current")

	m = m.WithExecCursor(1)
	view = m.View()
	assert.Contains(t, view, "Execution History")
	assert.Contains(t, view, "Latest")
	assert.Contains(t, view, "500")
}

func TestView_ResponseHistoryPopup_UsesFixedScrollableWindow(t *testing.T) {
	now := time.Now()
	var executions []*domain.Execution
	for i := 0; i < 12; i++ {
		executions = append(executions, &domain.Execution{
			ID:          strings.Join([]string{"ex", strconv.Itoa(i)}, "-"),
			RequestID:   "req-1",
			StatusCode:  200,
			CompletedAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}

	m := newModel(defaultConfig()).
		WithFocus(tui.ResponsePane).
		WithExecutions(executions).
		WithExecCursor(11)

	popup := m.HistoryPopupView(120, 30)
	assert.Contains(t, popup, "Execution History")
	assert.Contains(t, popup, "↑ more above")
	assert.NotContains(t, popup, "↓ more below")
	assert.LessOrEqual(
		t,
		lipgloss.Height(popup),
		11,
		"history popup should stay capped instead of growing with content",
	)
}

func TestView_RequestHints_UseConfiguredBindingsAndAliasPolicy(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.EditURL = "U"
	cfg.Keybindings.MethodNext = "n"
	cfg.Keybindings.MethodPrev = "p"
	cfg.Keybindings.SendRequest = "ctrl+s"
	cfg.Keybindings.EditBody = "B"
	cfg.Keybindings.EditHeaders = "H"
	cfg.Keybindings.EnvOpen = "E"
	cfg.Keybindings.EnvPrev = "ctrl+left"
	cfg.Keybindings.EnvNext = "ctrl+right"

	m := newModel(cfg).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	view := m.View()
	assert.Contains(t, view, "[U] url")
	assert.Contains(t, view, "[n/p] cycle method")
	assert.Contains(t, view, "[Ctrl+S/⌘+Enter] send")
	assert.Contains(t, view, "[B] body")
	assert.Contains(t, view, "[H] headers")
	assert.Contains(t, view, "[E] env")
	assert.Contains(t, view, "[Ctrl+←/Ctrl+→] cycle env")
}

func TestView_RequestPane_AuthSummaryRenderedOnce(t *testing.T) {
	cfg := defaultConfig()
	req := &domain.Request{
		ID:         "req-auth",
		Name:       "get-users",
		Method:     "POST",
		URL:        "https://example.test/users",
		AuthType:   domain.AuthTypeBasic,
		AuthConfig: `{"username":"alice","password":"secret"}`,
		Body:       `{"user_id":"42"}`,
	}

	m := newModel(cfg).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithMethod(req.Method).
		WithURLValue(req.URL)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	view := m.View()
	assert.Equal(t, 1, strings.Count(view, "Auth:"), "auth summary must render only once")
	assert.Contains(t, view, "Auth: Basic")
}

func TestView_ResponseHints_UseConfiguredBindings(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.TabBody = "B"
	cfg.Keybindings.TabHeaders = "H"
	cfg.Keybindings.TabRaw = "T"
	cfg.Keybindings.TabPrev = "ctrl+left"
	cfg.Keybindings.TabNext = "ctrl+right"
	cfg.Keybindings.ResponseRetry = "ctrl+r"
	cfg.Keybindings.ResponseUp = "K"
	cfg.Keybindings.ResponseDown = "J"

	m := newModel(cfg).
		WithFocus(tui.ResponsePane).
		WithResponse(&exec.ExecuteResult{StatusCode: 200, Status: "200 OK", Body: []byte(`{"ok":true}`), Duration: 5 * time.Millisecond, Size: 11})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	view := m.View()
	assert.Contains(t, view, "[B] Body")
	assert.Contains(t, view, "[H] Headers")
	assert.Contains(t, view, "[T] Raw")
	assert.Contains(t, view, "[Ctrl+←/Ctrl+→] view")
	assert.Contains(t, view, "[Ctrl+R] retry")
}

func TestView_StatusBar_UsesConfiguredBindings(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.Quit = "Q"
	cfg.Keybindings.Help = "H"
	cfg.Keybindings.Search = "S"
	cfg.Keybindings.FocusSidebar = "!"
	cfg.Keybindings.FocusRequest = "@"
	cfg.Keybindings.FocusResponse = "#"
	cfg.Keybindings.PaneNext = "ctrl+n"
	cfg.Keybindings.PanePrev = "ctrl+p"

	m := newModel(cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	status := m.View()
	assert.Contains(t, status, "[Q] quit")
	assert.Contains(t, status, "[H] help")
	assert.Contains(t, status, "[S] search")
	assert.Contains(t, status, "[!/@/#] pane")
	assert.Contains(t, status, "[Ctrl+N/Ctrl+P] cycle")
}

func TestView_HelpOverlay_UsesConfiguredBindings(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.HelpDown = "n"
	cfg.Keybindings.HelpUp = "p"
	cfg.Keybindings.HelpEdit = "i"
	cfg.Keybindings.HelpClose = "c"
	cfg.Keybindings.HelpReset = "x"
	cfg.Keybindings.HelpResetAll = "X"
	cfg.Keybindings.HelpUnbind = "u"

	m := newModel(cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})

	view := m.View()
	assert.Contains(t, view, "[i] edit")
	assert.Contains(t, view, "[x] reset")
	assert.Contains(t, view, "[X] reset all")
	assert.Contains(t, view, "[c] close")
	assert.Contains(t, view, "[p/n] navigate")
}

func TestHelpOverlay_ResetAll_ShowsConfirmationDiff(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.HelpClose = "c"
	cfg.Keybindings.HelpResetAll = "X"
	cfg.Keybindings.Quit = "Q"
	cfg.Keybindings.SidebarAdd = "Z"

	m := tui.New(tui.Deps{Config: cfg, ConfigDir: t.TempDir()})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	view := m.View()
	assert.Contains(t, view, "Reset all keybindings to defaults?")
	assert.Contains(t, view, "updated to their default values")
	assert.Contains(t, view, "quit: Q -> q")
	assert.Contains(t, view, "new collection: Z -> A")
	assert.Contains(t, view, "[Enter] confirm")
	assert.Contains(t, view, "[c] cancel")
	assert.NotContains(t, view, "Global")
}

func TestHelpOverlay_ResetAll_RequiresConfirmation(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.HelpClose = "c"
	cfg.Keybindings.HelpResetAll = "X"
	cfg.Keybindings.Quit = "Q"

	m := tui.New(tui.Deps{Config: cfg, ConfigDir: t.TempDir()})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	assert.Contains(t, m.View(), "Reset all keybindings to defaults?")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.NotContains(t, m.View(), "Reset all keybindings to defaults?")
	assert.Contains(t, m.View(), "quit                 q")
	assert.Equal(t, "Reset all keybindings to defaults", m.StatusSuccess())
}

func TestView_RequestPane_UsesAvailableHeightForPreview(t *testing.T) {
	body := strings.Join([]string{
		"{",
		`  "line1": true,`,
		`  "line2": true,`,
		`  "line3": true,`,
		`  "line4": true,`,
		`  "line5": true`,
		"}",
	}, "\n")

	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveRequest(&domain.Request{
			ID:   "req-1",
			Name: "Long Preview",
			Body: body,
		})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	view := m.View()
	assert.Contains(t, view, `"line5": true`)
	assert.NotContains(t, view, "  ...")
}

func TestView_RequestPane_PutsRequestNameAfterTitle(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveRequest(&domain.Request{
			ID:     "req-1",
			Name:   "get-users",
			Method: "POST",
			URL:    "https://example.com",
		}).
		WithMethod("POST").
		WithURLValue("https://example.com")
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	view := m.View()
	assert.Contains(t, view, "Request get-users")
	assert.NotContains(t, view, "\n  get-users\n")
}

func TestView_ResponseHeaders_RenderDeterministicallyAcrossRenders(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.ResponsePane).
		WithResponseTab(tui.HeadersTab).
		WithResponse(&exec.ExecuteResult{
			StatusCode: 200,
			Status:     "200 OK",
			Duration:   10 * time.Millisecond,
			Size:       42,
			Headers: http.Header{
				"X-Canary-Context": {"ctx"},
				"Date":             {"Wed, 17 Jun 2026 16:42:26 GMT"},
				"Content-Length":   {"143"},
				"Content-Type":     {"application/json; charset=utf-8"},
				"Server":           {"quark-test"},
			},
		})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	first := m.View()
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, m.View(), "render %d should be identical", i+1)
	}

	contentLengthIdx := strings.Index(first, "Content-Length")
	contentTypeIdx := strings.Index(first, "Content-Type")
	dateIdx := strings.Index(first, "Date")
	canaryIdx := strings.Index(first, "X-Canary-Context")
	require.NotEqual(t, -1, contentLengthIdx)
	require.NotEqual(t, -1, contentTypeIdx)
	require.NotEqual(t, -1, dateIdx)
	serverIdx := strings.Index(first, "Server")
	require.NotEqual(t, -1, serverIdx)
	require.NotEqual(t, -1, canaryIdx)
	assert.True(t, contentTypeIdx < contentLengthIdx)
	assert.True(t, contentLengthIdx < dateIdx)
	assert.True(t, dateIdx < serverIdx)
	assert.True(t, serverIdx < canaryIdx)
}

func TestView_ImportPreviewHeaders_RenderDeterministicallyAcrossRenders(t *testing.T) {
	m := newModel(defaultConfig()).
		WithMode(tui.ImportMode).
		WithImportPreview(&curl.ImportResult{
			Method: "POST",
			URL:    "https://example.test/import",
			Headers: map[string]string{
				"X-Canary-Context": "ctx",
				"Date":             "Wed, 17 Jun 2026 16:42:26 GMT",
				"Content-Length":   "143",
				"Content-Type":     "application/json; charset=utf-8",
				"Server":           "quark-test",
			},
		})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	first := m.View()
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, m.View(), "render %d should be identical", i+1)
	}

	contentLengthIdx := strings.Index(first, "Content-Length")
	contentTypeIdx := strings.Index(first, "Content-Type")
	dateIdx := strings.Index(first, "Date")
	canaryIdx := strings.Index(first, "X-Canary-Context")
	serverIdx := strings.Index(first, "Server")
	require.NotEqual(t, -1, contentLengthIdx)
	require.NotEqual(t, -1, contentTypeIdx)
	require.NotEqual(t, -1, dateIdx)
	require.NotEqual(t, -1, serverIdx)
	require.NotEqual(t, -1, canaryIdx)
	assert.True(t, contentTypeIdx < contentLengthIdx)
	assert.True(t, contentLengthIdx < dateIdx)
	assert.True(t, dateIdx < serverIdx)
	assert.True(t, serverIdx < canaryIdx)
}

// --- Esc cancels in-flight request ---
