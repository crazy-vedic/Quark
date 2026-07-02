package tui_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/tui"
)

type captureExecutor struct {
	last *domain.Request
}

func (e *captureExecutor) Execute(
	_ context.Context,
	req *domain.Request,
) (*exec.ExecuteResult, error) {
	cloned := *req
	e.last = &cloned
	return &exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       []byte(`{"ok":true}`),
		Duration:   5 * time.Millisecond,
		Size:       11,
	}, nil
}

type fakeEnvReader struct {
	global *domain.Environment
	envs   map[string]*domain.Environment
	byCol  map[string][]*domain.Environment
}

func (r *fakeEnvReader) GetEnvironment(_ context.Context, id string) (*domain.Environment, error) {
	if env, ok := r.envs[id]; ok {
		return env, nil
	}
	return nil, errors.New("not found")
}

func (r *fakeEnvReader) GetGlobalEnvironment(context.Context) (*domain.Environment, error) {
	return r.global, nil
}

func (r *fakeEnvReader) ListEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return r.byCol[collectionID], nil
}

func (r *fakeEnvReader) ListCollectionEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return r.byCol[collectionID], nil
}

func (r *fakeEnvReader) ListAllEnvironments(context.Context) ([]*domain.Environment, error) {
	var all []*domain.Environment
	for _, envs := range r.byCol {
		all = append(all, envs...)
	}
	return all, nil
}

const (
	col1 = "col-1"
)

// newModel returns a bare Model with no dependencies and the given config.
func newModel(cfg config.Config) tui.Model {
	return tui.New(tui.Deps{Config: cfg})
}

func defaultConfig() config.Config {
	cfg := config.Default("")
	cfg.UI.DefaultMethod = "GET"
	return cfg
}

func update(t *testing.T, m tui.Model, msg tea.Msg) tui.Model {
	t.Helper()
	updated, _ := m.Update(msg)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	return model
}

// --- collectionsLoadedMsg ---

func TestUpdate_CollectionsLoaded_StoresCollections(t *testing.T) {
	m := newModel(defaultConfig())
	cols := []*domain.Collection{
		{ID: col1, Name: "Alpha"},
		{ID: "col-2", Name: "Beta"},
	}
	m = update(t, m, tui.CollectionsLoadedMsg(cols))

	assert.Equal(t, 2, len(m.Collections()))
	assert.Equal(t, "Alpha", m.Collections()[0].Name)
}

func TestSearchModal_LongResultsStayWithinViewportAndKeepTitleVisible(t *testing.T) {
	m := newModel(defaultConfig()).
		WithMode(tui.SearchMode).
		WithSearchInputValue("notif")
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 16})
	m = update(t, m, tui.SearchResultsMsg([]*search.SearchHit{
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

func TestCommandPalette_FiltersAndOpensSchedulePrompt(t *testing.T) {
	m := newModel(defaultConfig()).WithActiveRequest(&domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	})
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	require.Equal(t, tui.SearchMode, m.Mode())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(">sched")})

	view := m.View()
	assert.Contains(t, view, "Command palette")
	assert.Contains(t, view, "Schedule request")
	require.Len(t, m.CommandResults(), 1)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.ScheduleMode, m.Mode())
}

func TestUpdate_CollectionsLoaded_ResetsCursor(t *testing.T) {
	m := newModel(defaultConfig())
	m = m.WithColCursor(5) // stale cursor
	m = update(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{
		{ID: col1, Name: "A"},
	}))
	assert.Equal(t, 0, m.ColCursor())
}

func TestUpdate_CollectionsLoaded_Empty_NoOp(t *testing.T) {
	m := newModel(defaultConfig())
	m = update(t, m, tui.CollectionsLoadedMsg(nil))
	assert.Empty(t, m.Collections())
}

// --- requestsLoadedMsg ---

func TestUpdate_RequestsLoaded_StoresRequests(t *testing.T) {
	m := newModel(defaultConfig())
	// Set up a collection so activeCollectionID matches the incoming message.
	m = update(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{
		{ID: col1, Name: "Alpha"},
	}))
	reqs := []*domain.Request{
		{ID: "req-1", Name: "List Users", Method: "GET"},
		{ID: "req-2", Name: "Create User", Method: "POST"},
	}
	m = update(t, m, tui.RequestsLoadedMsg(col1, reqs))

	// Requests are stored in collectionRequests for all expanded collections.
	assert.Equal(t, 2, len(m.CollectionRequests()[col1]))
	// And also in m.requests when the active collection matches.
	assert.Equal(t, 2, len(m.Requests()))
	assert.Equal(t, "List Users", m.Requests()[0].Name)
}

func TestUpdate_RequestsLoaded_DoesNotResetCursor(t *testing.T) {
	m := newModel(defaultConfig())
	m = update(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{
		{ID: col1, Name: "Alpha"},
	}))
	m = update(t, m, tui.RequestsLoadedMsg(col1, []*domain.Request{
		{ID: "r1", Name: "A", Method: "GET"},
	}))
	// reqCursor stays at -1 (on the collection) until user navigates into requests.
	assert.Equal(t, -1, m.ReqCursor())
}

// --- errLoadMsg ---

func TestUpdate_ErrLoad_SetsErr(t *testing.T) {
	m := newModel(defaultConfig())
	sentinel := errors.New("db down")
	m = update(t, m, tui.ErrLoadMsg(sentinel))

	require.NotNil(t, m.Err())
	assert.ErrorIs(t, m.Err(), sentinel)
}

// --- searchResultsMsg ---

func TestUpdate_SearchResults_Stored(t *testing.T) {
	m := newModel(defaultConfig())
	hits := []*search.SearchHit{
		{Request: &domain.Request{ID: "r1", Name: "Users"}, Score: 1.0},
	}
	m = update(t, m, tui.SearchResultsMsg(hits))
	assert.Len(t, m.SearchResults(), 1)
}

func TestView_SearchModal_ScrollsLongResults(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.SearchMode)
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: 14})

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
	m = update(t, m, tui.SearchResultsMsg(hits))

	first := m.View()
	assert.Contains(t, first, "Result 1")
	assert.Contains(t, first, "Result 3")
	assert.NotContains(t, first, "Result 4")
	assert.Contains(t, first, "↓ more below")

	for i := 0; i < 4; i++ {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
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
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("invoice")})
	m = update(t, m, tui.SearchResultsMsg([]*search.SearchHit{
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

func TestUpdate_Key1_FocusSidebar(t *testing.T) {
	m := newModel(defaultConfig())
	m = m.WithFocus(tui.RequestPane)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

func TestUpdate_Key2_FocusRequest(t *testing.T) {
	m := newModel(defaultConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	assert.Equal(t, tui.RequestPane, m.Focus())
}

func TestUpdate_Key3_FocusResponse(t *testing.T) {
	m := newModel(defaultConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	assert.Equal(t, tui.ResponsePane, m.Focus())
}

func TestUpdate_Tab_CyclesPanesForward(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.SidebarPane)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.RequestPane, m.Focus())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.ResponsePane, m.Focus())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

func TestUpdate_ShiftTab_CyclesPanesBackward(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.SidebarPane)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tui.ResponsePane, m.Focus())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tui.RequestPane, m.Focus())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

// --- Sidebar navigation ---

func TestUpdate_SidebarJ_MovesCursorDown(t *testing.T) {
	m := newModel(defaultConfig())
	m = m.WithCollections([]*domain.Collection{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}).WithFocus(tui.SidebarPane)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 1, m.ColCursor())
}

func TestUpdate_SidebarJ_ClampedAtEnd(t *testing.T) {
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: "a"}}).
		WithFocus(tui.SidebarPane).
		WithColCursor(0)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 0, m.ColCursor(), "cursor must not exceed collection count")
}

func TestUpdate_SidebarK_MovesCursorUp(t *testing.T) {
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: "a"}, {ID: "b"}}).
		WithFocus(tui.SidebarPane).
		WithColCursor(1)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	assert.Equal(t, 0, m.ColCursor())
}

func TestUpdate_SidebarK_ClampedAtZero(t *testing.T) {
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: "a"}}).
		WithFocus(tui.SidebarPane).
		WithColCursor(0)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	assert.Equal(t, 0, m.ColCursor(), "cursor must not go below 0")
}

func TestUpdate_SidebarEnter_ShiftsFocusToRequest(t *testing.T) {
	reqs := []*domain.Request{
		{ID: "r1", Name: "Get Users", Method: "GET", URL: "http://example.com"},
	}
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: col1, Name: "A"}}).
		WithCollectionRequests(map[string][]*domain.Request{col1: reqs}).
		WithRequests(reqs).
		WithFocus(tui.SidebarPane)
	// Expand collection and navigate into first request.
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}) // expand
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // down into request

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.RequestPane, m.Focus())
}

// --- Method cycling ---

func TestUpdate_MethodM_CyclesGETtoPOST(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithMethod("GET")

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	assert.Equal(t, "POST", m.Method())
}

func TestUpdate_MethodM_CyclesAllMethods(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.RequestPane).WithMethod("GET")
	methods := []string{"POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "GET"}
	for _, want := range methods {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
		assert.Equal(t, want, m.Method())
	}
}

func TestUpdate_MethodM_CyclesBackward(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.RequestPane).WithMethod("GET")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	assert.Equal(t, "OPTIONS", m.Method())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	assert.Equal(t, "HEAD", m.Method())
}

// --- Search modal ---

func TestUpdate_SlashKey_OpensSearchMode(t *testing.T) {
	m := newModel(defaultConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	assert.Equal(t, tui.SearchMode, m.Mode())
}

func TestUpdate_EscInSearchMode_ReturnsNormal(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.SearchMode)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
}

// --- Help overlay ---

func TestUpdate_QuestionMark_OpensHelpMode(t *testing.T) {
	m := newModel(defaultConfig())
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.Equal(t, tui.HelpMode, m.Mode())
}

func TestUpdate_EscInHelpMode_ReturnsNormal(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.HelpMode)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
}

func TestUpdate_NavKeyInHelpMode_MovesCursor(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.HelpMode)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, tui.HelpMode, m.Mode())
	assert.Equal(t, 1, m.HelpCursor())
}

// --- Response pane tabs ---

func TestUpdate_ResponseB_SetsBodyTab(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.ResponsePane).
		WithResponseTab(tui.HeadersTab)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	assert.Equal(t, tui.BodyTab, m.ResponseTab())
}

func TestUpdate_ResponseH_SetsHeadersTab(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.ResponsePane)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	assert.Equal(t, tui.HeadersTab, m.ResponseTab())
}

func TestUpdate_ResponseR_SetsRawTab(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.ResponsePane)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	assert.Equal(t, tui.RawTab, m.ResponseTab())
}

// --- HTTP response clears loading, switches to response pane ---

func TestUpdate_HttpResponse_SwitchesToResponsePane(t *testing.T) {
	m := newModel(defaultConfig()).WithLoading(true).WithFocus(tui.RequestPane)
	result := &exec.ExecuteResult{StatusCode: 200, Body: []byte(`{"ok":true}`)}
	m = update(t, m, tui.HttpResponseMsg(result))

	assert.False(t, m.Loading())
	assert.Equal(t, tui.ResponsePane, m.Focus())
	assert.Equal(t, tui.BodyTab, m.ResponseTab())
}

func TestUpdate_ResponseHistory_ArrowKeysFollowVisibleListDirection(t *testing.T) {
	executions := []*domain.Execution{
		{ID: "newest", RequestID: "req-1", StatusCode: 200, CompletedAt: time.Now()},
		{
			ID:          "older",
			RequestID:   "req-1",
			StatusCode:  500,
			CompletedAt: time.Now().Add(-time.Minute),
		},
	}
	m := newModel(defaultConfig()).
		WithFocus(tui.ResponsePane).
		WithExecutions(executions).
		WithExecCursor(0)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(
		t,
		1,
		m.ExecCursor(),
		"down should move to an older execution shown lower in the list",
	)

	m = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(
		t,
		0,
		m.ExecCursor(),
		"up should move back toward the newer execution shown higher in the list",
	)
}

func TestUpdate_ResponseRetry_ReplaysHistoricalSnapshotWithoutMutatingRequestPane(t *testing.T) {
	executor := &captureExecutor{}
	cfg := defaultConfig()
	m := tui.New(tui.Deps{
		Config:   cfg,
		Executor: executor,
	})
	m = m.WithFocus(tui.ResponsePane).
		WithMethod("POST").
		WithURLValue("https://current.example.com/live").
		WithActiveRequest(&domain.Request{
			ID:           "req-1",
			CollectionID: "col-1",
			Name:         "Current",
			Method:       "POST",
			URL:          "https://current.example.com/live",
			Headers:      `{"X-Current":"true"}`,
			Body:         `{"current":true}`,
		}).
		WithExecutions([]*domain.Execution{
			{
				ID:              "newer",
				RequestID:       "req-1",
				RequestSnapshot: `{"method":"POST","url":"https://current.example.com/live","headers":"{\"X-Current\":\"true\"}","body":"{\"current\":true}"}`,
			},
			{
				ID:              "older",
				RequestID:       "req-1",
				RequestSnapshot: `{"method":"GET","url":"https://older.example.com/replay","headers":"{\"X-Retry\":\"true\"}","body":"{\"older\":true}"}`,
			},
		}).
		WithExecCursor(1)
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	require.True(t, model.Loading(), "retry should dispatch a new request")
	require.NotNil(t, cmd, "retry should dispatch an execute command")

	msg := cmd()
	require.NotNil(t, msg)
	require.NotNil(t, executor.last, "retry should replay a request through the executor")
	assert.Equal(t, "req-1", executor.last.ID)
	assert.Equal(t, "col-1", executor.last.CollectionID)
	assert.Equal(t, "GET", executor.last.Method)
	assert.Equal(t, "https://older.example.com/replay", executor.last.URL)
	assert.Equal(t, `{"X-Retry":"true"}`, executor.last.Headers)
	assert.Equal(t, `{"older":true}`, executor.last.Body)
	assert.Equal(
		t,
		"https://current.example.com/live",
		model.URLValue(),
		"retry must not overwrite the request pane URL",
	)
	require.NotNil(t, model.ActiveRequest())
	assert.Equal(
		t,
		"https://current.example.com/live",
		model.ActiveRequest().URL,
		"retry must not mutate the active request template",
	)

	model = update(t, model, msg)
	assert.Equal(
		t,
		"https://current.example.com/live",
		model.URLValue(),
		"response handling must still leave request pane state untouched",
	)
}

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
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	assert.Contains(t, m.View(), "Reset all keybindings to defaults?")

	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.NotContains(t, m.View(), "Reset all keybindings to defaults?")
	assert.Contains(t, m.View(), "quit                 q")
	assert.Equal(t, "Reset all keybindings to defaults", m.StatusSuccess())
}

func TestView_EnvModal_UsesConfiguredBindings(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "Global"}
	global.SetVars(map[string]string{})
	collectionEnv := &domain.Environment{ID: "env-1", CollectionID: col1, Name: "default"}
	collectionEnv.SetVars(map[string]string{})

	reader := &fakeEnvReader{
		global: global,
		envs: map[string]*domain.Environment{
			"global": global,
			"env-1":  collectionEnv,
		},
		byCol: map[string][]*domain.Environment{
			col1: {collectionEnv},
		},
	}

	cfg := defaultConfig()
	cfg.Keybindings.EnvTabPrev = "u"
	cfg.Keybindings.EnvTabNext = "o"
	cfg.Keybindings.EnvUp = "p"
	cfg.Keybindings.EnvDown = "n"
	cfg.Keybindings.EnvAdd = "z"
	cfg.Keybindings.EnvCreate = "G"
	cfg.Keybindings.EnvEdit = "i"
	cfg.Keybindings.EnvDelete = "D"
	cfg.Keybindings.EnvSave = "v"
	cfg.Keybindings.EnvCancel = "x"
	cfg.Keybindings.EnvEditSwitchField = "w"
	cfg.Keybindings.EnvEditConfirm = "c"

	m := newModel(cfg).WithEnvReader(reader)
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = update(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{{ID: col1, Name: "Alpha"}}))
	m = m.WithFocus(tui.RequestPane)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	view := m.View()
	assert.Contains(t, view, "Press [z] to add")
	assert.Contains(t, view, "[u/o] tabs")
	assert.Contains(t, view, "[p/n] nav")
	assert.Contains(t, view, "[z] add var")
	assert.Contains(t, view, "[G] new env")
	assert.Contains(t, view, "[i] edit")
	assert.Contains(t, view, "[D]")
	assert.Contains(t, view, "delete")
	assert.Contains(t, view, "[v] save")
	assert.Contains(t, view, "[x] close")

	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	view = m.View()
	assert.Contains(t, view, "[w] switch")
	assert.Contains(t, view, "[c] confirm")
	assert.Contains(t, view, "[x] cancel")
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
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	view := m.View()
	assert.Contains(t, view, "Request get-users")
	assert.NotContains(t, view, "\n  get-users\n")
}

func TestView_EnvModal_ScrollsLongVariableLists(t *testing.T) {
	global := &domain.Environment{ID: "global", Name: "Global"}
	global.SetVars(map[string]string{})

	vars := make(map[string]string)
	for i := 1; i <= 8; i++ {
		vars["KEY_"+strconv.Itoa(i)] = "value-" + strconv.Itoa(i)
	}
	collectionEnv := &domain.Environment{ID: "env-1", CollectionID: col1, Name: "default"}
	collectionEnv.SetVars(vars)

	reader := &fakeEnvReader{
		global: global,
		envs: map[string]*domain.Environment{
			"global": global,
			"env-1":  collectionEnv,
		},
		byCol: map[string][]*domain.Environment{
			col1: {collectionEnv},
		},
	}

	m := newModel(defaultConfig()).WithEnvReader(reader)
	m = update(t, m, tea.WindowSizeMsg{Width: 90, Height: 14})
	m = update(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{{ID: col1, Name: "Alpha"}}))
	m = m.WithFocus(tui.RequestPane)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyTab})

	first := m.View()
	assert.Contains(t, first, "KEY_1")
	assert.Contains(t, first, "KEY_3")
	assert.NotContains(t, first, "KEY_4")
	assert.Contains(t, first, "↓ more below")

	for i := 0; i < 4; i++ {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}

	scrolled := m.View()
	assert.NotContains(t, scrolled, "KEY_1")
	assert.Contains(t, scrolled, "KEY_5")
	assert.Contains(t, scrolled, "↑ more above")
	assert.Contains(t, scrolled, "↓ more below")
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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

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
	m = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

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

func TestUpdate_EscInNormalMode_CancelsRequest(t *testing.T) {
	cancelled := false
	m := newModel(defaultConfig()).
		WithLoading(true).
		WithCancel(func() { cancelled = true })

	update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, cancelled)
}

// --- WindowSize ---

func TestUpdate_WindowSize_Stored(t *testing.T) {
	m := newModel(defaultConfig())
	m = update(t, m, tea.WindowSizeMsg{Width: 200, Height: 50})
	assert.Equal(t, 200, m.Width())
	assert.Equal(t, 50, m.Height())
}
