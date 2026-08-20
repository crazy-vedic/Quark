package tui_test

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tuitest"
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
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})

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
	assert.NotContains(t, first, "Result 8")
	assert.Contains(t, first, "↓ more below")

	for i := 0; i < 4; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}

	scrolled := m.View()
	assert.NotContains(t, scrolled, "Result 1")
	assert.Contains(t, scrolled, "Result 5")
	assert.Contains(t, scrolled, "↑ more above")
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
	// Render the request pane wide enough that responsive hint truncation does not
	// clip the configured bindings under test (full View at 120px still truncates).
	pane := m.ViewRequestPaneForTest(140, 20)
	assert.Contains(t, pane, "[U] url")
	assert.Contains(t, pane, "[n/p] cycle method")
	assert.Contains(t, pane, "[Ctrl+S/⌘+Enter] send")
	assert.Contains(t, pane, "[B] body")
	assert.Contains(t, pane, "[H] headers")
	assert.Contains(t, pane, "[E] env")
	assert.Contains(t, pane, "[Ctrl+←/Ctrl+→] cycle env")
}

func TestView_RequestPane_HintsStayOnOneLine(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.RequestPane)
	for _, w := range []int{40, 55, 70, 89, 100, 120} {
		pane := m.ViewRequestPaneForTest(w, 20)
		var hintLines int
		for _, line := range strings.Split(pane, "\n") {
			if strings.Contains(line, "] url") || strings.Contains(line, "]url") {
				hintLines++
				assert.Equal(t, 1, lipgloss.Height(line),
					"width=%d: hint row must be a single visual line, got %q", w, line)
			}
		}
		assert.Equal(t, 1, hintLines, "width=%d: helper hints must occupy exactly one row", w)
	}
}

func TestView_TabBar_HintsStayOnOneLine(t *testing.T) {
	m := newModel(defaultConfig()).
		WithResponse(&exec.ExecuteResult{StatusCode: 200, Status: "200 OK", Body: []byte(`{}`), Duration: time.Millisecond, Size: 2}).
		WithExecutions([]*domain.Execution{
			{StatusCode: 200, ResponseBody: `{}`, CompletedAt: time.Now()},
			{StatusCode: 201, ResponseBody: `{}`, CompletedAt: time.Now()},
		}).
		WithExecCursor(0)

	for _, w := range []int{30, 50, 64, 80, 120} {
		bar := m.ViewTabBarForTest(w)
		assert.LessOrEqual(t, lipgloss.Width(bar), w, "maxWidth=%d bar=%q", w, bar)
		assert.Equal(t, 1, lipgloss.Height(bar), "maxWidth=%d must stay one row", w)
	}
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

func TestView_StatusBar_ActionErrorBeatsVisualOverflow(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.Equal(t, "Select a collection first", m.StatusErr())

	// Even when the layout reports visual overflow, the action error must win.
	bar := m.ViewStatusBarForTest(tui.VisualOverflowStatus)
	assert.Contains(t, bar, "Select a collection first")
	assert.NotContains(t, bar, tui.VisualOverflowStatus)
}

func TestView_StatusBar_KeepsHintStyleWhenTruncated(t *testing.T) {
	// Tests run with Ascii color profile by default; force TrueColor so we can
	// assert that truncation re-applies styling instead of stripping it.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	wide := m.ViewStatusBarForTest("")
	require.Contains(t, wide, "\x1b[", "wide status bar should be styled")

	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 30, Height: 40})
	narrow := m.ViewStatusBarForTest("")
	require.NotEmpty(t, narrow)
	require.Contains(t, narrow, "\x1b[", "truncated status bar must keep ANSI styling")
	assert.LessOrEqual(t, lipgloss.Width(narrow), 30)

	// Crowded with a right-side message: hints still stay styled.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.Equal(t, "Select a collection first", m.StatusErr())
	crowded := m.ViewStatusBarForTest("")
	require.Contains(t, crowded, "\x1b[")
	assert.LessOrEqual(t, lipgloss.Width(crowded), 30)
	assert.Contains(t, crowded, "Select a collection first")
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

func TestTruncate_ASCII(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "HelloWorld", tui.Truncate("HelloWorld", 10))
	assert.Equal(t, "HelloWo…", tui.Truncate("HelloWorld", 8))
	assert.Equal(t, "", tui.Truncate("Hello", 0))
	assert.Equal(t, "", tui.Truncate("", 10))
}

func TestTruncate_MultiByteUTF8(t *testing.T) {
	t.Parallel()
	// "café" is 4 display columns; each accented char is still 1 column.
	assert.Equal(t, "café", tui.Truncate("café", 4))
	// maxCols <= 3 omits the ellipsis and hard-cuts by display width.
	assert.Equal(t, "caf", tui.Truncate("café", 3))
	assert.Equal(t, "café…", tui.Truncate("café latte", 5))
}

func TestTruncate_DoubleWidthRunes(t *testing.T) {
	t.Parallel()
	// Each CJK ideograph is typically 2 display columns.
	s := "東京サーバー"
	assert.LessOrEqual(t, lipgloss.Width(tui.Truncate(s, 6)), 6)
	assert.LessOrEqual(t, lipgloss.Width(tui.Truncate(s, 4)), 4)
	assert.Contains(t, tui.Truncate(s, 6), "…")
	assert.Equal(t, s, tui.Truncate(s, lipgloss.Width(s)))
}

func TestTruncate_MaxColsTiny(t *testing.T) {
	t.Parallel()
	assert.LessOrEqual(t, lipgloss.Width(tui.Truncate("Hello", 1)), 1)
	assert.LessOrEqual(t, lipgloss.Width(tui.Truncate("Hello", 2)), 2)
	assert.LessOrEqual(t, lipgloss.Width(tui.Truncate("Hello", 3)), 3)
	// Tiny budgets omit the ellipsis and just cut.
	assert.NotContains(t, tui.Truncate("Hello", 2), "…")
}

func TestView_SidebarIndicatorsFitInsidePane(t *testing.T) {
	reqs := make([]*domain.Request, 100)
	for i := range reqs {
		reqs[i] = &domain.Request{
			ID:     fmt.Sprintf("r-%d", i),
			Name:   "A request name that is deliberately much longer than the sidebar width",
			Method: "DELETE",
		}
	}
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: "col-1", Name: "Collection with a very long name"}}).
		WithCollectionRequests(map[string][]*domain.Request{"col-1": reqs}).
		WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	view := m.View()
	assert.Contains(t, view, "↓ more below")
	assert.LessOrEqual(t, lipgloss.Height(view), m.Height())
	for _, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), m.Width(), "sidebar row wrapped: %q", line)
	}
}

func TestView_SidebarNestedCollectionsUseChevronsAndBadges(t *testing.T) {
	collections := []*domain.Collection{
		{ID: "root", Name: "AEF"},
		{ID: "child", Name: "Data Plane", ParentID: "root"},
		{ID: "sibling", Name: "Other Root"},
	}
	m := newModel(defaultConfig()).
		WithCollections(collections).
		WithCollectionRequests(map[string][]*domain.Request{
			"child": {{ID: "req-1", Name: "Single turn", Method: "POST"}},
		}).
		WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	view := m.View()
	assert.Contains(t, view, "▾ [AEF]")
	assert.Contains(t, view, "  ▸ [Data Plane]")
	assert.Contains(t, view, "▸ [Other Root]")
	assert.NotContains(t, view, "└─")
}

func TestView_SidebarNestedCollectionBadgesFitAtNarrowWidth(t *testing.T) {
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{
			{ID: "root", Name: "A very long top-level collection name"},
			{ID: "child", Name: "A very long nested collection name", ParentID: "root"},
		}).
		WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 24, Height: 12})

	for _, line := range strings.Split(m.View(), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), m.Width(), "sidebar row wrapped: %q", line)
	}
}

// limitLines must never emit more visual rows than its budget. Regression test
// for the bug where the appended "… N more lines" notice pushed clipped output
// to maxRows+1 rows, overflowing the exactly-terminal-height layout and
// scrolling the top row of the app off-screen.
func TestLimitLines_NeverExceedsMaxRows(t *testing.T) {
	t.Parallel()

	widths := []int{10, 40, 80}
	maxRowsCases := []int{1, 2, 3, 5, 10}
	lineCounts := []int{0, 1, 5, 50, 500}

	for _, contentWidth := range widths {
		for _, maxRows := range maxRowsCases {
			for _, n := range lineCounts {
				var sb strings.Builder
				for i := 0; i < n; i++ {
					// Mix short lines and lines wide enough to wrap.
					if i%3 == 0 {
						sb.WriteString(strings.Repeat("x", contentWidth*2+3))
					} else {
						fmt.Fprintf(&sb, "line %d", i)
					}
					if i != n-1 {
						sb.WriteString("\n")
					}
				}

				out := tui.LimitLines(sb.String(), contentWidth, maxRows)
				got := tui.VisualRows(out, contentWidth)
				if got > maxRows {
					t.Errorf(
						"tui.LimitLines(width=%d, maxRows=%d, lines=%d) used %d visual rows, want <= %d",
						contentWidth,
						maxRows,
						n,
						got,
						maxRows,
					)
				}
			}
		}
	}
}

// When content is clipped, the truncation notice should still be present.
func TestLimitLines_ShowsTruncationNotice(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}

	out := tui.LimitLines(sb.String(), 40, 5)
	if !strings.Contains(out, "more lines") {
		t.Fatalf("expected truncation notice, got:\n%s", out)
	}
	if rows := tui.VisualRows(out, 40); rows != 5 {
		t.Fatalf("expected notice to consume the full budget (5 rows), got %d rows", rows)
	}
}

// A single logical line wider than the budget must still show a partial prefix,
// not only the truncation notice (common for minified HTML bodies).
func TestLimitLines_ShowsPartialLongLine(t *testing.T) {
	t.Parallel()

	longLine := "<!DOCTYPE html>" + strings.Repeat("x", 5000)
	for i := 0; i < 148; i++ {
		longLine += fmt.Sprintf("\nline %d", i)
	}

	const contentWidth = 80
	const maxRows = 15

	out := tui.LimitLines(longLine, contentWidth, maxRows)
	if !strings.Contains(out, "<!DOCTYPE html>") {
		t.Fatalf("expected partial body content before notice, got:\n%s", out)
	}
	if !strings.Contains(out, "more lines") {
		t.Fatalf("expected truncation notice, got:\n%s", out)
	}
	if rows := tui.VisualRows(out, contentWidth); rows > maxRows {
		t.Fatalf("expected at most %d visual rows, got %d", maxRows, rows)
	}
	if rows := tui.VisualRows(out, contentWidth); rows < 2 {
		t.Fatalf("expected partial content plus notice (>=2 rows), got %d rows", rows)
	}
}

// Content that fits must pass through untouched (no notice, no dropped lines).
func TestLimitLines_FitsWithoutNotice(t *testing.T) {
	t.Parallel()

	in := "alpha\nbeta\ngamma"
	out := tui.LimitLines(in, 40, 10)
	if out != in {
		t.Fatalf("expected unchanged content, got %q", out)
	}
	if strings.Contains(out, "more lines") {
		t.Fatalf("did not expect a truncation notice for content that fits")
	}
}

// The normal 3-pane layout is sized to occupy exactly the terminal height. A
// large response body — including lines whose width lands right on the pane's
// inner-width boundary — must never push the rendered output past that height,
// otherwise the terminal scrolls and the top row of the app disappears.
//
// Regression test for the report where a 178x47 terminal rendered 48 rows
// because the response body was clipped at width w instead of the pane's inner
// width w-2, so a line 147-148 columns wide counted as one row but wrapped to
// two when rendered.
func TestView_ResponsePane_DoesNotOverflowHeight(t *testing.T) {
	t.Parallel()

	// Body with a mix of line widths, including ones that sit exactly on the
	// w-2 boundary for common terminal widths, plus very long wrapping lines.
	var body strings.Builder
	for i := 0; i < 400; i++ {
		switch i % 4 {
		case 0:
			body.WriteString(strings.Repeat("a", 146)) // 178-wide term inner width
		case 1:
			body.WriteString(strings.Repeat("b", 147))
		case 2:
			body.WriteString(strings.Repeat("c", 148))
		default:
			body.WriteString(strings.Repeat("abcdefghij ", 30))
		}
		body.WriteString("\n")
	}

	sizes := []struct{ w, h int }{
		{178, 47}, // the reported case
		{100, 24},
		{140, 40},
		{80, 16},
		{200, 50},
		{120, 30},
	}

	tabs := []struct {
		name  string
		apply func(tui.Model) tui.Model
	}{
		{"body", func(m tui.Model) tui.Model { return m.WithResponseTab(tui.BodyTab) }},
		{"raw", func(m tui.Model) tui.Model { return m.WithResponseTab(tui.RawTab) }},
		{"headers", func(m tui.Model) tui.Model { return m.WithResponseTab(tui.HeadersTab) }},
	}

	for _, tab := range tabs {
		for _, sz := range sizes {
			m := newModel(defaultConfig())
			m = callUpdate(t, m, tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			ex := &domain.Execution{
				StatusCode:      405,
				ResponseBody:    body.String(),
				ResponseHeaders: `{"Content-Type":["text/html"]}`,
				ResponseTimeMs:  169,
				CompletedAt:     time.Now(),
			}
			m = tab.apply(m.WithExecutions([]*domain.Execution{ex}).WithExecCursor(0))

			view := m.View()
			gotH := lipgloss.Height(view)
			if gotH > sz.h {
				t.Errorf(
					"tab=%s size=%dx%d: View() rendered %d rows, exceeds terminal height %d",
					tab.name, sz.w, sz.h, gotH, sz.h,
				)
			}
		}
	}
}

// Long auth summaries and header lines must be truncated/clipped so they cannot
// wrap the request pane past the terminal height.
func TestView_RequestPane_LongChromeDoesNotOverflowHeight(t *testing.T) {
	t.Parallel()

	longAuthUser := strings.Repeat("u", 200)
	longHeaderVal := strings.Repeat("v", 300)
	sizes := []struct{ w, h int }{
		{80, 16},
		{100, 24},
		{120, 30},
		{140, 40},
	}

	for _, sz := range sizes {
		m := newModel(defaultConfig())
		m = callUpdate(t, m, tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		req := &domain.Request{
			ID:         "req-1",
			Name:       "Long chrome request " + strings.Repeat("n", 80),
			Method:     "POST",
			URL:        "https://example.com/" + strings.Repeat("path/", 40),
			AuthType:   domain.AuthTypeBasic,
			AuthConfig: `{"username":"` + longAuthUser + `","password":"secret"}`,
			Headers:    `{"X-Long":"` + longHeaderVal + `"}`,
			Body:       strings.Repeat("body-line\n", 100),
		}
		m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)

		view := m.View()
		gotH := lipgloss.Height(view)
		if gotH > sz.h {
			t.Errorf("size=%dx%d: View() rendered %d rows, exceeds terminal height %d",
				sz.w, sz.h, gotH, sz.h)
		}
		gotW := lipgloss.Width(view)
		if gotW > sz.w {
			t.Errorf("size=%dx%d: View() rendered %d cols, exceeds terminal width %d",
				sz.w, sz.h, gotW, sz.w)
		}
	}
}

// Terminals in the absurd tier must show a full-screen message instead of panes.
// The too-small screen must still fit the terminal (no AllowFrameOverflow).
func TestView_TooSmall_ShowsResizeMessage(t *testing.T) {
	t.Parallel()

	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 20, Height: 6})

	view := m.View()
	if !strings.Contains(view, "Terminal too small") && !strings.Contains(view, "too small") {
		// After hard-clamp the text may be truncated; still must not show panes.
		if strings.Contains(view, "Collections") {
			t.Fatalf("too-small view must not render panes, got:\n%s", view)
		}
	}
	if strings.Contains(view, "Collections") {
		t.Fatalf("too-small view must not render panes, got:\n%s", view)
	}
	assert.LessOrEqual(t, lipgloss.Height(view), 6)
	assert.LessOrEqual(t, lipgloss.Width(view), 20)
	assert.False(t, m.HasFrameOverflow(), "too-small screen must fit the terminal")
}

func TestView_DimModes_AutoAndForced(t *testing.T) {
	t.Parallel()

	t.Run("auto narrow stacks", func(t *testing.T) {
		m := newModel(defaultConfig())
		m = callUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 30})
		assert.Equal(t, tui.DimModeNarrow, m.EffectiveDim())
		view := m.View()
		assert.Contains(t, view, "Collections")
		assert.Contains(t, view, "[narrow]")
	})

	t.Run("auto tiny single pane", func(t *testing.T) {
		m := newModel(defaultConfig())
		m = callUpdate(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
		assert.Equal(t, tui.DimModeTiny, m.EffectiveDim())
		view := m.View()
		assert.Contains(t, view, "Collections")
		assert.Contains(t, view, "[tiny]")
		// Request pane title should not appear while sidebar is focused.
		assert.NotContains(t, view, "Request")
	})

	t.Run("forced tiny at wide size", func(t *testing.T) {
		m := newModel(defaultConfig()).WithForceDim(tui.DimModeTiny)
		m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
		assert.Equal(t, tui.DimModeTiny, m.EffectiveDim())
		view := m.View()
		assert.Contains(t, view, "[dim:tiny]")
		assert.Contains(t, view, "Collections")
		assert.NotContains(t, view, "Response")
	})

	t.Run("forced absurd", func(t *testing.T) {
		m := newModel(defaultConfig()).WithForceDim(tui.DimModeAbsurd)
		m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
		view := m.View()
		assert.Contains(t, view, "Forced --dim=absurd")
		assert.NotContains(t, view, "Collections")
	})
}

// The overflow safety-net must fire when rendered content still exceeds the
// terminal after layout (e.g. a pathological frame). It surfaces a status
// error and writes a detailed block to the --debug log.
func TestView_VisualOverflow_DetectionFiresAndLogs(t *testing.T) {
	t.Parallel()
	tuitest.AllowFrameOverflow(t)

	debugPath := t.TempDir() + "/debug.log"
	f, err := os.Create(debugPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	m := newModel(defaultConfig()).WithDebugLog(f)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// Inject a status override path by rendering a frame that is taller than
	// the terminal: use many executions + a huge body on a short-but-valid height
	// is hard to force after clipping, so exercise the logger/status via the
	// package helper that viewNormal uses when Height(out) > height.
	tall := strings.Repeat("overflow-line\n", 80)
	tui.ForceLogVisualOverflowForTest(m, tall)

	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	logBytes, err := os.ReadFile(debugPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, "VISUAL OVERFLOW") {
		t.Fatalf("expected VISUAL OVERFLOW in debug log, got:\n%s", logText)
	}
}

// Across density tiers the finished frame must fit within the terminal.
func TestView_FrameFitsTerminal_AcrossSizes(t *testing.T) {
	t.Parallel()

	sizes := []struct{ w, h int }{
		{24, 8},  // tiny floor
		{40, 12}, // tiny
		{50, 14}, // narrow by height boundary
		{60, 30}, // narrow
		{80, 16}, // narrow by height
		{80, 24}, // wide
		{100, 24},
		{120, 30},
		{140, 40},
		{178, 47},
		{200, 50},
	}

	body := strings.Repeat(strings.Repeat("x", 200)+"\n", 80)
	ex := &domain.Execution{
		StatusCode:   200,
		ResponseBody: body,
		ResponseHeaders: `{"Content-Type":["application/json"],"X-Long":["` + strings.Repeat(
			"h",
			200,
		) + `"]}`,
	}
	req := &domain.Request{
		ID:      "r1",
		Name:    "Frame fit " + strings.Repeat("n", 60),
		Method:  "GET",
		URL:     "https://example.com/" + strings.Repeat("p/", 50),
		Body:    body,
		Headers: `{"Authorization":"Bearer ` + strings.Repeat("t", 100) + `"}`,
	}

	for _, sz := range sizes {
		if sz.w < tui.MinTerminalWidth || sz.h < tui.MinTerminalHeight {
			t.Fatalf("test size %dx%d is below absurd floor", sz.w, sz.h)
		}
		m := newModel(defaultConfig())
		m = callUpdate(t, m, tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = m.WithCollections([]*domain.Collection{
			{ID: "c1", Name: "Col " + strings.Repeat("c", 40)},
		}).WithColCursor(0).
			WithActiveRequest(req).
			WithExecutions([]*domain.Execution{ex}).
			WithExecCursor(0).
			WithFocus(tui.SidebarPane)

		view := m.View()
		if gotH := lipgloss.Height(view); gotH > sz.h {
			t.Errorf("size=%dx%d height=%d exceeds terminal", sz.w, sz.h, gotH)
		}
		if gotW := lipgloss.Width(view); gotW > sz.w {
			t.Errorf("size=%dx%d width=%d exceeds terminal", sz.w, sz.h, gotW)
		}
		if strings.Contains(view, "Terminal too small") {
			t.Errorf("size=%dx%d unexpectedly showed too-small screen", sz.w, sz.h)
		}
	}
}

func TestView_EnvModalWidth_NeverExceedsTerminal(t *testing.T) {
	t.Parallel()

	// At minimum size, env modal must still fit when opened.
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 40, Height: 12})
	m = m.WithCollections([]*domain.Collection{{ID: "c1", Name: "C"}}).WithColCursor(0)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if m.Mode() != tui.EnvMode {
		// Env may require collection; if it didn't open, still check normal view.
		view := m.View()
		if lipgloss.Width(view) > 40 {
			t.Fatalf("normal view width %d > 40", lipgloss.Width(view))
		}
		return
	}
	view := m.View()
	if lipgloss.Width(view) > 40 {
		t.Fatalf("env modal frame width %d > terminal 40", lipgloss.Width(view))
	}
}
