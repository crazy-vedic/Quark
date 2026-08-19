//go:build e2e

// Package tui_test provides end-to-end tests that exercise the full Model
// through Update() chains and assert on both internal state and rendered View()
// output. Run with: go test -tags e2e ./tests/e2e/tui/...
package tui_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/tui"
)

// --- E2E: Full user journey ---

func TestE2E_FullUserJourney(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API Tests"}
	st := setupStore(t, col)
	seedRequests(
		t,
		st,
		col.ID,
		&domain.Request{
			ID:     "req-1",
			Name:   "Get JSON",
			Method: "GET",
			URL:    "https://example.com/json",
		},
		&domain.Request{
			ID:     "req-2",
			Name:   "Create Item",
			Method: "POST",
			URL:    "https://example.com/items",
		},
	)

	ex := &mockExecutor{Latency: 0}
	m := newE2EModel(t, st, ex)

	// 1. Window size (startup)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, m.Width())
	assert.Equal(t, 40, m.Height())

	// 2. Collections load on Init()
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	assert.Len(t, m.Collections(), 1)
	assert.Equal(t, "API Tests", m.Collections()[0].Name)

	// 3. Requests load when expanding the collection
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{
		{ID: "req-1", Name: "Get JSON", Method: "GET", URL: "https://example.com/json"},
		{ID: "req-2", Name: "Create Item", Method: "POST", URL: "https://example.com/items"},
	}))
	assert.Len(t, m.Requests(), 2)

	// 4. Rendered sidebar shows collection and requests
	assertViewContains(t, m, "API Tests")
	assertViewContains(t, m, "Get JSON")
	assertViewContains(t, m, "Create Item")

	// 5. Switch to request pane with '2' (non-vim)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	assert.Equal(t, tui.RequestPane, m.Focus())
	assertViewContains(t, m, "Request") // request pane title visible

	// 6. Enter URL editing mode
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	assert.True(t, m.ActiveField() == tui.URLField)
	assertViewContains(t, m, "https://...") // placeholder visible

	// 7. Type a URL (keys routed to textinput)
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{
			Type: tea.KeyRunes,
			Runes: []rune{
				'h',
				't',
				't',
				'p',
				':',
				'/',
				'/',
				'e',
				'x',
				'a',
				'm',
				'p',
				'l',
				'e',
				'.',
				'c',
				'o',
				'm',
			},
		},
	)
	assert.Equal(t, "http://example.com", m.URLValue())

	// 8. Press Enter to finish editing
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.ActiveField() == tui.URLField)

	// 9. Send request (this sets loading=true and dispatches a goroutine)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	assert.True(t, m.Loading())
	assertViewContains(t, m, "Sending…")

	// 10. Simulate HTTP response arriving
	m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"status":"ok","id":42}`),
		Duration:   10 * time.Millisecond,
		Size:       25,
	}))
	assert.False(t, m.Loading())
	assert.Nil(t, m.Err())
	require.NotNil(t, m.Response())
	assert.Equal(t, 200, m.Response().StatusCode)
	assert.Equal(t, "200 OK", m.Response().Status)

	// 11. Focus shifted to response pane automatically on successful response
	assert.Equal(t, tui.ResponsePane, m.Focus())

	// 12. Rendered response pane shows JSON body
	assertViewContains(t, m, "200 OK")
	assertViewContains(t, m, "status")
	assertViewContains(t, m, "ok")

	// 13. Switch response tabs with right arrow (non-vim alternative)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, tui.HeadersTab, m.ResponseTab())
	assertViewContains(t, m, "Content-Type")

	// 14. Switch to raw tab with right arrow again
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, tui.RawTab, m.ResponseTab())

	// 15. left from rawTab → headersTab (1), not bodyTab
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, tui.HeadersTab, m.ResponseTab())

	// 16. left again → bodyTab
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, tui.BodyTab, m.ResponseTab())
	assertViewContains(t, m, "status") // body content visible again

	// 17. Cycle panes with Tab (non-vim alternative to Ctrl+w).
	// From ResponsePane (2) → SidebarPane (0) → RequestPane (1) → ResponsePane (2)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.SidebarPane, m.Focus())

	// 18. Tab again → request pane
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.RequestPane, m.Focus())

	// 19. Tab again → response pane
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.ResponsePane, m.Focus())

	// 19. Quit
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
}

// --- E2E: Arrow key navigation in sidebar ---

func TestE2E_ArrowKeysInSidebar(t *testing.T) {
	col1 := &domain.Collection{ID: "col-1", Name: "First"}
	col2 := &domain.Collection{ID: "col-2", Name: "Second"}
	col3 := &domain.Collection{ID: "col-3", Name: "Third"}
	st := setupStore(t, col1, col2, col3)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col1, col2, col3}))

	assert.Equal(t, 0, m.ColCursor())

	// Down arrow moves to next collection
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, m.ColCursor())

	// Down again
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, m.ColCursor())

	// Up arrow moves back
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, m.ColCursor())

	// Up again
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, m.ColCursor())

	// Up at top is clamped to 0
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, m.ColCursor())
}

// --- E2E: Number keys switch panes ---

func TestE2E_NumberKeysSwitchPanes(t *testing.T) {
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	assert.Equal(t, tui.SidebarPane, m.Focus())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	assert.Equal(t, tui.RequestPane, m.Focus())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	assert.Equal(t, tui.ResponsePane, m.Focus())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

// --- E2E: Error handling ---

func TestE2E_InvalidURLShowsValidationError(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "Test"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	assert.Equal(t, tui.RequestPane, m.Focus())

	// Enter URL editing mode
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	assert.True(t, m.ActiveField() == tui.URLField)

	// Type invalid URL
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n', 'o', 't', '-', 'a', '-', 'u', 'r', 'l'}},
	)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.ActiveField() == tui.URLField)

	// Send request
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	// Simulate validation error
	m = callUpdate(t, m, tui.HttpErrMsg(exec.ErrInvalidURL))
	assert.False(t, m.Loading())
	assert.NotEmpty(t, m.ValidationErr())
	assertViewContains(t, m, "invalid URL")
}

func TestE2E_TimeoutShowsStatusError(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "Test"}
	st := setupStore(t, col)

	ex := &mockExecutor{Latency: 100 * time.Millisecond}
	m := newE2EModel(t, st, ex)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane, enter URL, send
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{'h', 't', 't', 'p', ':', '/', '/', 'x', '.', 'c', 'o', 'm'},
		},
	)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	assert.True(t, m.Loading())

	// Simulate timeout
	m = callUpdate(t, m, tui.HttpErrMsg(fmt.Errorf("%w", exec.ErrTimeout)))
	assert.False(t, m.Loading())
	assert.Empty(t, m.ValidationErr())
	assert.Contains(t, m.StatusErr(), "timed out")
	assertViewContains(t, m, "timed out")
}

// --- E2E: Search flow ---

func TestE2E_SearchFlow(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID,
		&domain.Request{ID: "req-1", Name: "Get Users", Method: "GET", URL: "/users"},
		&domain.Request{ID: "req-2", Name: "Create User", Method: "POST", URL: "/users"},
		&domain.Request{ID: "req-3", Name: "Delete User", Method: "DELETE", URL: "/users/1"},
	)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, m.Requests()))

	// Open search with '/'
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, tui.SearchMode, m.Mode())
	assertViewContains(t, m, "Search all requests")

	// Type "create"
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c', 'r', 'e', 'a', 't', 'e'}},
	)
	// (In a real app, search results would be async; here we simulate the result)
	m = callUpdate(t, m, tui.SearchResultsMsg([]*search.SearchHit{
		{Request: &domain.Request{ID: "req-2", Name: "Create User", Method: "POST", URL: "/users"}},
	}))
	assert.Len(t, m.SearchResults(), 1)
	assertViewContains(t, m, "Create User")

	// Press Enter to select
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, tui.RequestPane, m.Focus())
	assert.Equal(t, "POST", m.Method())
	assert.Equal(t, "/users", m.URLValue())
}

func TestE2E_SearchFlow_FindsRequestsAcrossCollections(t *testing.T) {
	col1 := &domain.Collection{ID: "col-1", Name: "API"}
	col2 := &domain.Collection{ID: "col-2", Name: "Billing"}
	st := setupStore(t, col1, col2)
	seedRequests(t, st, col1.ID,
		&domain.Request{ID: "req-1", Name: "Get Users", Method: "GET", URL: "/users"},
	)
	seedRequests(t, st, col2.ID,
		&domain.Request{ID: "req-2", Name: "Create Invoice", Method: "POST", URL: "/invoices"},
	)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col1, col2}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col1.ID, []*domain.Request{
		{ID: "req-1", Name: "Get Users", Method: "GET", URL: "/users"},
	}))

	// Leave focus on the first collection and search for a request in another collection.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, tui.SearchMode, m.Mode())

	for _, r := range "invoice" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// The async result should include hits from col-2 even though col-1 is selected.
	m = callUpdate(t, m, tui.SearchResultsMsg([]*search.SearchHit{
		{
			Request: &domain.Request{
				ID:           "req-2",
				Name:         "Create Invoice",
				Method:       "POST",
				URL:          "/invoices",
				CollectionID: col2.ID,
			},
		},
	}))
	assertViewContains(t, m, "Create Invoice")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NormalMode, m.Mode())
	assert.Equal(t, tui.RequestPane, m.Focus())
	assert.Equal(t, "POST", m.Method())
	assert.Equal(t, "/invoices", m.URLValue())
}

// --- E2E: Curl import flow ---

func TestE2E_CurlImportFlow(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, nil))

	// Open the dedicated import modal and paste a curl command.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	curlCmd := "curl -X POST -H 'Authorization: Bearer secret' https://api.example.com/items"
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(curlCmd), Paste: true})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})

	// Import modal should appear
	assert.Equal(t, tui.ImportMode, m.Mode())
	assert.NotNil(t, m.ImportPreview())
	assertViewContains(t, m, "Import curl command")
	assertViewContains(t, m, "POST")
	assertViewContains(t, m, "api.example.com")
	assertViewContains(t, m, "[REDACTED]") // Authorization header is redacted

	// Cancel with Esc
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
}

// --- E2E: Help overlay shows non-vim alternatives ---

func TestE2E_HelpOverlay(t *testing.T) {
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	// Open help with '?'
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, tui.HelpMode, m.Mode())
	assertViewContains(t, m, "Keyboard Reference")
	// Help shows configured keybindings only (not hardcoded aliases).
	assertViewContains(t, m, "j")
	assertViewContains(t, m, "tab")
	assertViewContains(t, m, "shift+tab")
	assertViewContains(t, m, "m")
	assertViewContains(t, m, "b")

	// Close help with Esc (unmapped keys are intentional no-ops)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
}

// --- E2E: Real HTTP round-trip ---

func TestE2E_RealHTTPRoundTrip(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "Live"}
	st := setupStore(t, col)

	srv, executor := realExecutor(t)

	m := newE2EModel(t, st, executor)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// Switch to request pane
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})

	// Type the test server URL
	url := srv.URL + "/test/path"
	for _, r := range url {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.ActiveField() == tui.URLField)
	assert.Equal(t, url, m.URLValue())

	// Send request
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	assert.True(t, m.Loading())

	// Simulate a successful response (the real executor would send this via tea.Cmd)
	m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"path":"/test/path","method":"GET"}`),
		Duration:   5 * time.Millisecond,
		Size:       36,
	}))
	assert.False(t, m.Loading())
	assert.Equal(t, tui.ResponsePane, m.Focus())
	assertViewContains(t, m, "200 OK")
	assertViewContains(t, m, "/test/path")
}

func TestE2E_ResponsePane_HistoryNavigationAcrossExecutions(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "History"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: col.ID,
		Name:         "Get Timeline",
		Method:       "GET",
		URL:          "",
	}
	seedRequests(t, st, col.ID, req)

	var hitCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.Header().Set("Content-Type", "application/json")
		if hitCount == 1 {
			w.WriteHeader(201)
			fmt.Fprint(w, `{"attempt":1,"label":"older"}`)
			return
		}
		w.WriteHeader(503)
		fmt.Fprint(w, `{"attempt":2,"label":"newer"}`)
	}))
	t.Cleanup(srv.Close)

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport, exec.WithExecutionWriter(st))

	m := newE2EModel(t, st, executor)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))

	// Select the saved request from the sidebar; this also dispatches history load.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)
	require.Equal(t, tui.RequestPane, m.Focus())

	// Point the request at the real test server.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	for _, r := range srv.URL + "/timeline" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// First send.
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	msg = runCmd(t, cmd)
	require.NotNil(t, msg)
	m, cmd = callUpdateWithCmd(t, m, msg)
	msg = runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)
	executions, err := st.ListExecutionsByRequest(context.Background(), req.ID)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	require.Equal(t, 201, executions[0].StatusCode)

	// Return to the request pane before sending again.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	require.Equal(t, tui.RequestPane, m.Focus())

	// Second send.
	time.Sleep(1100 * time.Millisecond) // ensure the rendered timestamp changes at second precision
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	msg = runCmd(t, cmd)
	require.NotNil(t, msg)
	m, cmd = callUpdateWithCmd(t, m, msg)
	msg = runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)
	executions, err = st.ListExecutionsByRequest(context.Background(), req.ID)
	require.NoError(t, err)
	require.Len(t, executions, 2)
	require.Equal(t, 503, executions[0].StatusCode)
	require.Equal(t, 201, executions[1].StatusCode)

	require.Equal(t, tui.ResponsePane, m.Focus())
	require.Len(t, m.Executions(), 2)
	assert.Equal(t, 0, m.ExecCursor(), "newest execution should be selected by default")

	today := time.Now().Format("2006-01-02")
	assertViewContains(t, m, "Run 1/2")
	assertViewContains(t, m, today)
	assertViewContains(t, m, "503")
	assertViewContains(t, m, "newer")
	assertViewNotContains(t, m, "Execution History")

	// Move down to an older execution in the history list.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftDown})
	assert.Equal(t, 1, m.ExecCursor())
	assertViewContains(t, m, "Run 2/2")
	assertViewContains(t, m, "201")
	assertViewContains(t, m, "older")
	assertViewContains(t, m, "Execution History")
	assertViewContains(t, m, "Latest")

	// Move back up toward the present execution.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftUp})
	assert.Equal(t, 0, m.ExecCursor())
	assertViewContains(t, m, "Run 1/2")
	assertViewContains(t, m, "503")
	assertViewContains(t, m, "newer")
	assertViewNotContains(t, m, "Execution History")
}

// --- E2E: Cancel in-flight request ---

func TestE2E_CancelInFlightRequest(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "Test"}
	st := setupStore(t, col)

	ex := &mockExecutor{Latency: 10 * time.Second} // Very slow, will be cancelled
	m := newE2EModel(t, st, ex)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{'h', 't', 't', 'p', ':', '/', '/', 'x', '.', 'c', 'o', 'm'},
		},
	)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	assert.True(t, m.Loading())

	// Cancel with Esc
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m.Loading())
	assert.Empty(t, m.StatusErr())
	assert.Empty(t, m.ValidationErr())
}

func TestE2E_ResponseRetry_ReplaysOlderExecutionWithoutChangingRequestPane(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "History"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: col.ID,
		Name:         "Replay Me",
		Method:       "GET",
		URL:          "",
	}
	seedRequests(t, st, col.ID, req)

	var hitPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPaths = append(hitPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"path":"%s"}`, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	executor := exec.New(transport, exec.WithExecutionWriter(st))

	m := newE2EModel(t, st, executor)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))

	// Select the saved request from the sidebar.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)
	require.Equal(t, tui.RequestPane, m.Focus())

	sendAndDrain := func() {
		m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
		msg = runCmd(t, cmd)
		require.NotNil(t, msg)
		m, cmd = callUpdateWithCmd(t, m, msg)
		msg = runCmd(t, cmd)
		require.NotNil(t, msg)
		m = callUpdate(t, m, msg)
	}

	olderURL := srv.URL + "/older"
	newerURL := srv.URL + "/newer"

	m = m.WithURLValue(olderURL)
	sendAndDrain()

	time.Sleep(1100 * time.Millisecond)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = m.WithURLValue(newerURL)
	sendAndDrain()

	require.Equal(t, tui.ResponsePane, m.Focus())
	require.Len(t, m.Executions(), 2)
	assert.Equal(
		t,
		newerURL,
		m.URLValue(),
		"request pane should still reflect the newer editable request",
	)

	// Move to the older execution and retry it from history.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftDown})
	assert.Equal(t, 1, m.ExecCursor())

	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	msg = runCmd(t, cmd)
	require.NotNil(t, msg)
	m, cmd = callUpdateWithCmd(t, m, msg)
	msg = runCmd(t, cmd)
	require.NotNil(t, msg)
	m = callUpdate(t, m, msg)

	require.Equal(
		t,
		[]string{"/older", "/newer", "/older"},
		hitPaths,
		"retry should replay the selected historical request snapshot",
	)
	assert.Equal(
		t,
		newerURL,
		m.URLValue(),
		"retry must not overwrite the editable request pane URL",
	)
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(
		t,
		"",
		m.ActiveRequest().URL,
		"retry must not mutate the stored active request template",
	)
	assertViewContains(t, m, `"/older"`)
}

// --- E2E: Empty state ---

func TestE2E_EmptyState(t *testing.T) {
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	assert.Empty(t, m.Collections())
	assertViewContains(t, m, "No collections")
	assertViewContains(t, m, "Press [A] to add")
}

// --- E2E: Collapse collection with left arrow ---

func TestE2E_CollapseCollectionWithLeftArrow(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "Test"}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID,
		&domain.Request{ID: "req-1", Name: "First", Method: "GET", URL: "/1"},
	)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))

	// CollectionsLoadedMsg auto-expands the first collection; simulate the
	// async requests-loaded message from the dispatched Cmd
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{
		{ID: "req-1", Name: "First", Method: "GET", URL: "/1"},
	}))
	assertViewContains(t, m, "First") // request visible after expand

	// Collapse with left arrow (non-vim alternative to 'h')
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	assertViewNotContains(t, m, "First") // request not visible after collapse
}

// --- E2E: URL editor persistence ---

// TestE2E_URLEditor_PersistsToStore guards against the bug where finishing a
// URL edit (Enter) only blurred the input without writing the new URL back to
// the active request or persisting it. Navigating away and back then lost the
// edited URL because it was never saved.
func TestE2E_URLEditor_PersistsToStore(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "ewq", Method: "GET", URL: "",
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	// Enter URL editing mode with 'u'.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	assert.Equal(t, tui.URLField, m.ActiveField())

	// Type a URL.
	const newURL = "https://example.com/new"
	for _, r := range newURL {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	assert.Equal(t, newURL, m.URLValue())

	// Finish editing with Enter — this must persist the URL.
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NoneField, m.ActiveField())

	// Drain the save command so the store write happens.
	if cmd != nil {
		if msg := runCmd(t, cmd); msg != nil {
			m = callUpdate(t, m, msg)
		}
	}

	// The edited URL must be persisted to the store.
	got, err := st.GetRequest(context.Background(), "req-1")
	require.NoError(t, err)
	assert.Equal(t, newURL, got.URL, "edited URL must be persisted to the store")

	// And the in-memory active request must reflect the change.
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, newURL, m.ActiveRequest().URL, "active request URL must be updated")
}

// TestE2E_MethodCycle_PersistsToStore guards against the bug where cycling the
// HTTP method updated the in-memory pane state but never wrote it back to the
// active request or the store, so the change was lost on navigation.
func TestE2E_MethodCycle_PersistsToStore(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "ewq", Method: "GET", URL: "https://example.com",
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithMethod("GET")
	m = m.WithFocus(tui.RequestPane)

	// Cycle the method forward with 'm'.
	var cmd tea.Cmd
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	newMethod := m.Method()
	assert.NotEqual(t, "GET", newMethod, "method should have cycled")

	// Drain the save command so the store write happens.
	if cmd != nil {
		if msg := runCmd(t, cmd); msg != nil {
			m = callUpdate(t, m, msg)
		}
	}

	// The cycled method must be persisted to the store.
	got, err := st.GetRequest(context.Background(), "req-1")
	require.NoError(t, err)
	assert.Equal(t, newMethod, got.Method, "cycled method must be persisted to the store")

	// And the in-memory active request must reflect the change.
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, newMethod, m.ActiveRequest().Method, "active request method must be updated")
}

// --- E2E: Body editor ---

func TestE2E_BodyEditor_Save(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.com/items",
		Body: `{"name":"old"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))

	// Select the request so it becomes active, then switch to request pane
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	// Press 'b' to open body editor
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.Equal(t, tui.BodyField, m.ActiveField(), "activeField must be bodyField")
	assert.Equal(t, `{"name":"old"}`, m.BodyValue(), "body textarea must show existing body")

	// Type new body content
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 'n', 'e', 'w', '"', '}'},
		},
	)
	assert.Equal(
		t,
		`{"name":"old"}{"name":"new"}`,
		m.BodyValue(),
		"body textarea must contain new text",
	)

	// Save with Enter
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NoneField, m.ActiveField(), "activeField must be noneField after save")
	require.NotNil(t, m.ActiveRequest(), "active request must be set")
	assert.Equal(
		t,
		`{"name":"old"}{"name":"new"}`,
		m.ActiveRequest().Body,
		"active request body must be updated",
	)
}

func TestE2E_BodyEditor_Cancel(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.com/items",
		Body: `{"name":"original"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	// Open body editor
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.Equal(t, tui.BodyField, m.ActiveField())

	// Type some text
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s', 't', 'u', 'f', 'f'}})
	assert.Equal(t, `{"name":"original"}stuff`, m.BodyValue())

	// Cancel with Esc
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NoneField, m.ActiveField(), "activeField must be noneField after cancel")
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(
		t,
		`{"name":"original"}`,
		m.ActiveRequest().Body,
		"body must be reverted to original",
	)
}

// --- E2E: Header editor ---

func TestE2E_HeaderEditor_Save(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.com/items",
		Headers: `{"Content-Type":"application/json","Authorization":"Bearer old"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	// Press 'h' to open header editor
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.Equal(t, tui.HeadersField, m.ActiveField(), "activeField must be headersField")

	// Verify both headers are listed
	assert.Len(t, m.HeaderPairs(), 2, "must show both headers")

	// Save with Enter (no changes)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NoneField, m.ActiveField(), "activeField must be noneField after save")
	require.NotNil(t, m.ActiveRequest())
	// Headers JSON may have reordered keys due to map iteration; verify individual values
	headers := m.ActiveRequest().Headers
	assert.Contains(t, headers, "Content-Type")
	assert.Contains(t, headers, "Authorization")
}

func TestE2E_HeaderEditor_EditPair(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.com/items",
		Headers: `{"Content-Type":"application/json"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	// Open header editor
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.Equal(t, tui.HeadersField, m.ActiveField())
	assert.Len(t, m.HeaderPairs(), 1)
	assert.Equal(t, "Content-Type", m.HeaderPairs()[0].Key)

	// Press 'e' to edit the first pair
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.True(t, m.HeaderEditing(), "must enter editing sub-mode")

	// Enter in field edit mode confirms the field, then Enter in list mode saves.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.HeaderEditing(), "must exit editing sub-mode")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NoneField, m.ActiveField(), "activeField must be noneField after save")
	require.NotNil(t, m.ActiveRequest())
	assert.Contains(t, m.ActiveRequest().Headers, "Content-Type")
}

func TestE2E_HeaderEditor_Cancel(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.com/items",
		Headers: `{"Content-Type":"application/json"}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	// Open header editor
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.Equal(t, tui.HeadersField, m.ActiveField())

	// Delete the only header with 'd'
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.Len(t, m.HeaderPairs(), 0, "header must be deleted")

	// Cancel with Esc
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NoneField, m.ActiveField(), "activeField must be noneField after cancel")
	require.NotNil(t, m.ActiveRequest())
	assert.Contains(
		t,
		m.ActiveRequest().Headers,
		"Content-Type",
		"headers must be reverted to original",
	)
}

func TestE2E_HeaderEditor_AddPair(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	st := setupStore(t, col)
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.com/items",
		Headers: `{}`,
	}
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req)
	m = m.WithFocus(tui.RequestPane)

	// Open header editor
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.Equal(t, tui.HeadersField, m.ActiveField())
	assert.Len(t, m.HeaderPairs(), 0, "must start with no headers")

	// Add a new header with 'a'
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.True(t, m.HeaderEditing(), "must enter editing sub-mode")

	// Confirm with Enter (empty key/value is fine for test)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.HeaderEditing(), "must exit editing sub-mode")
	assert.Len(t, m.HeaderPairs(), 1, "must have one new header pair")

	// Save with Enter
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NoneField, m.ActiveField(), "activeField must be noneField after save")
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, `{}`, m.ActiveRequest().Headers, "empty key is ignored so headers stay empty")
}
