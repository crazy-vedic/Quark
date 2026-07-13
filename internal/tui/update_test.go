package tui_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestUpdate_HttpResponse_ClearsLoadingState(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	result := &exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       []byte(`{"ok":true}`),
		Duration:   100 * time.Millisecond,
	}
	updated := callUpdate(t, m, tui.HttpResponseMsg(result))

	assert.False(t, updated.Loading(), "loading must be cleared after response")
	assert.Nil(t, updated.Err(), "err must be nil on success")
	assert.Equal(t, 200, updated.Response().StatusCode)
}

func TestUpdate_ErrTimeout_SetsStatusErr(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	updated := callUpdate(t, m, tui.HttpErrMsg(
		fmt.Errorf("wrapped: %w", exec.ErrTimeout),
	))

	assert.False(t, updated.Loading())
	assert.NotEmpty(t, updated.StatusErr(), "timeout must set status bar error")
	assert.Nil(t, updated.Err(), "timeout is Tier 2, not fatal")
}

func TestUpdate_ErrInvalidURL_SetsValidationErr(t *testing.T) {
	m := newTestModel()
	m = m.WithActiveRequest(&domain.Request{ID: "req-1", Method: "GET", URL: "://bad"})
	m = m.WithLoading(true)

	updated := callUpdate(t, m, tui.HttpErrMsg(
		fmt.Errorf("bad url: %w", exec.ErrInvalidURL),
	))

	assert.False(t, updated.Loading())
	assert.NotEmpty(t, updated.ValidationErr(), "invalid URL must set validation error")
	assert.Empty(t, updated.StatusErr())
}

func TestSelectRequest_SwitchingRequests_ShowsEachRequestsValidationErr(t *testing.T) {
	reqA := &domain.Request{ID: "a", Method: "GET", URL: "://bad"}
	reqB := &domain.Request{ID: "b", Method: "GET", URL: "https://example.com"}

	m := newTestModel().
		WithActiveRequest(reqA).
		WithRequestValidationErr("a", "invalid URL: ://bad")

	m, _ = m.SelectRequest(reqB)

	assert.Empty(t, m.ValidationErr(),
		"request B should not show request A's validation error")

	m, _ = m.SelectRequest(reqA)

	assert.Equal(t, "invalid URL: ://bad", m.ValidationErr(),
		"switching back to request A should restore its validation error")
}

func TestSelectRequest_SameRequest_KeepsValidationErr(t *testing.T) {
	reqA := &domain.Request{ID: "a", Method: "GET", URL: "://bad"}

	m := newTestModel().
		WithActiveRequest(reqA).
		WithValidationErr("invalid URL: ://bad")

	m, _ = m.SelectRequest(reqA)

	assert.NotEmpty(t, m.ValidationErr(),
		"validation error must remain when re-selecting the same request")
}

func TestUpdate_ErrCancelled_NoErrorShown(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	updated := callUpdate(t, m, tui.HttpErrMsg(
		fmt.Errorf("ctx: %w", exec.ErrRequestCancelled),
	))

	assert.False(t, updated.Loading())
	assert.Empty(t, updated.StatusErr(), "cancel must not show an error")
	assert.Nil(t, updated.Err())
}

func TestUpdate_EscKey_CancelsInFlightRequest(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	cancelled := false
	m = m.WithCancel(func() { cancelled = true })

	callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, cancelled, "Esc must call the cancel func")
}

func TestUpdate_EscKey_NoOp_WhenNotLoading(t *testing.T) {
	m := newTestModel()
	// Not loading, no cancel func.
	updated := callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, updated.Loading())
}

func TestUpdate_QuitKey(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd, "ctrl+c must return a Quit command")
}

func TestUpdate_UnexpectedError_SetsFatalErr(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	sentinel := errors.New("disk full")
	updated := callUpdate(t, m, tui.HttpErrMsg(sentinel))

	assert.False(t, updated.Loading())
	assert.NotNil(t, updated.Err(), "unexpected error must be stored")
	assert.ErrorIs(t, updated.Err(), sentinel)
}

func TestUpdate_WindowSize(t *testing.T) {
	m := newTestModel()
	updated := callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, updated.Width())
	assert.Equal(t, 40, updated.Height())
}

func TestUpdate_CollectionsLoaded_StoresCollections(t *testing.T) {
	m := newModel(defaultConfig())
	cols := []*domain.Collection{
		{ID: col1, Name: "Alpha"},
		{ID: "col-2", Name: "Beta"},
	}
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(cols))

	assert.Equal(t, 2, len(m.Collections()))
	assert.Equal(t, "Alpha", m.Collections()[0].Name)
}

func TestCommandPalette_FiltersAndOpensSchedulePrompt(t *testing.T) {
	m := newModel(defaultConfig()).WithActiveRequest(&domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	require.Equal(t, tui.SearchMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(">sched")})

	view := m.View()
	assert.Contains(t, view, "Command palette")
	assert.Contains(t, view, "Schedule request")
	require.Len(t, m.CommandResults(), 1)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.ScheduleMode, m.Mode())
}

func TestUpdate_CollectionsLoaded_ResetsCursor(t *testing.T) {
	m := newModel(defaultConfig())
	m = m.WithColCursor(5) // stale cursor
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{
		{ID: col1, Name: "A"},
	}))
	assert.Equal(t, 0, m.ColCursor())
}

func TestUpdate_CollectionsLoaded_Empty_NoOp(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))
	assert.Empty(t, m.Collections())
}

// --- requestsLoadedMsg ---

func TestUpdate_RequestsLoaded_StoresRequests(t *testing.T) {
	m := newModel(defaultConfig())
	// Set up a collection so activeCollectionID matches the incoming message.
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{
		{ID: col1, Name: "Alpha"},
	}))
	reqs := []*domain.Request{
		{ID: "req-1", Name: "List Users", Method: "GET"},
		{ID: "req-2", Name: "Create User", Method: "POST"},
	}
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col1, reqs))

	// Requests are stored in collectionRequests for all expanded collections.
	assert.Equal(t, 2, len(m.CollectionRequests()[col1]))
	// And also in m.requests when the active collection matches.
	assert.Equal(t, 2, len(m.Requests()))
	assert.Equal(t, "List Users", m.Requests()[0].Name)
}

func TestUpdate_RequestsLoaded_DoesNotResetCursor(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{
		{ID: col1, Name: "Alpha"},
	}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col1, []*domain.Request{
		{ID: "r1", Name: "A", Method: "GET"},
	}))
	// reqCursor stays at -1 (on the collection) until user navigates into requests.
	assert.Equal(t, -1, m.ReqCursor())
}

// --- errLoadMsg ---

func TestUpdate_ErrLoad_SetsErr(t *testing.T) {
	m := newModel(defaultConfig())
	sentinel := errors.New("db down")
	m = callUpdate(t, m, tui.ErrLoadMsg(sentinel))

	require.NotNil(t, m.Err())
	assert.ErrorIs(t, m.Err(), sentinel)
}

// --- searchResultsMsg ---

func TestUpdate_SearchResults_Stored(t *testing.T) {
	m := newModel(defaultConfig())
	hits := []*search.SearchHit{
		{Request: &domain.Request{ID: "r1", Name: "Users"}, Score: 1.0},
	}
	m = callUpdate(t, m, tui.SearchResultsMsg(hits))
	assert.Len(t, m.SearchResults(), 1)
}

func TestUpdate_Key1_FocusSidebar(t *testing.T) {
	m := newModel(defaultConfig())
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

func TestUpdate_Key2_FocusRequest(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	assert.Equal(t, tui.RequestPane, m.Focus())
}

func TestUpdate_Key3_FocusResponse(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	assert.Equal(t, tui.ResponsePane, m.Focus())
}

func TestUpdate_Tab_CyclesPanesForward(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.RequestPane, m.Focus())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.ResponsePane, m.Focus())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

func TestUpdate_ShiftTab_CyclesPanesBackward(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tui.ResponsePane, m.Focus())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tui.RequestPane, m.Focus())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

// --- Sidebar navigation ---

func TestUpdate_SidebarJ_MovesCursorDown(t *testing.T) {
	m := newModel(defaultConfig())
	m = m.WithCollections([]*domain.Collection{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}).WithFocus(tui.SidebarPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 1, m.ColCursor())
}

func TestUpdate_SidebarJ_ClampedAtEnd(t *testing.T) {
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: "a"}}).
		WithFocus(tui.SidebarPane).
		WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 0, m.ColCursor(), "cursor must not exceed collection count")
}

func TestUpdate_SidebarK_MovesCursorUp(t *testing.T) {
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: "a"}, {ID: "b"}}).
		WithFocus(tui.SidebarPane).
		WithColCursor(1)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	assert.Equal(t, 0, m.ColCursor())
}

func TestUpdate_SidebarK_ClampedAtZero(t *testing.T) {
	m := newModel(defaultConfig()).
		WithCollections([]*domain.Collection{{ID: "a"}}).
		WithFocus(tui.SidebarPane).
		WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
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
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}) // expand
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // down into request

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.RequestPane, m.Focus())
}

// --- Method cycling ---

func TestUpdate_MethodM_CyclesGETtoPOST(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithMethod("GET")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	assert.Equal(t, "POST", m.Method())
}

func TestUpdate_MethodM_CyclesAllMethods(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.RequestPane).WithMethod("GET")
	methods := []string{"POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "GET"}
	for _, want := range methods {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
		assert.Equal(t, want, m.Method())
	}
}

func TestUpdate_MethodM_CyclesBackward(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.RequestPane).WithMethod("GET")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	assert.Equal(t, "OPTIONS", m.Method())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	assert.Equal(t, "HEAD", m.Method())
}

// --- Search modal ---

func TestUpdate_SlashKey_OpensSearchMode(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	assert.Equal(t, tui.SearchMode, m.Mode())
}

func TestUpdate_EscInSearchMode_ReturnsNormal(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.SearchMode)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
}

// --- Help overlay ---

func TestUpdate_QuestionMark_OpensHelpMode(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.Equal(t, tui.HelpMode, m.Mode())
}

func TestUpdate_EscInHelpMode_ReturnsNormal(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.HelpMode)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode())
}

func TestUpdate_NavKeyInHelpMode_MovesCursor(t *testing.T) {
	m := newModel(defaultConfig()).WithMode(tui.HelpMode)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, tui.HelpMode, m.Mode())
	assert.Equal(t, 1, m.HelpCursor())
}

// --- Response pane tabs ---

func TestUpdate_ResponseB_SetsBodyTab(t *testing.T) {
	m := newModel(defaultConfig()).
		WithFocus(tui.ResponsePane).
		WithResponseTab(tui.HeadersTab)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	assert.Equal(t, tui.BodyTab, m.ResponseTab())
}

func TestUpdate_ResponseH_SetsHeadersTab(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.ResponsePane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	assert.Equal(t, tui.HeadersTab, m.ResponseTab())
}

func TestUpdate_ResponseR_SetsRawTab(t *testing.T) {
	m := newModel(defaultConfig()).WithFocus(tui.ResponsePane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	assert.Equal(t, tui.RawTab, m.ResponseTab())
}

// --- HTTP response clears loading, switches to response pane ---

func TestUpdate_HttpResponse_SwitchesToResponsePane(t *testing.T) {
	m := newModel(defaultConfig()).WithLoading(true).WithFocus(tui.RequestPane)
	result := &exec.ExecuteResult{StatusCode: 200, Body: []byte(`{"ok":true}`)}
	m = callUpdate(t, m, tui.HttpResponseMsg(result))

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

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(
		t,
		1,
		m.ExecCursor(),
		"down should move to an older execution shown lower in the list",
	)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
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
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

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

	model = callUpdate(t, model, msg)
	assert.Equal(
		t,
		"https://current.example.com/live",
		model.URLValue(),
		"response handling must still leave request pane state untouched",
	)
}

func TestUpdate_EscInNormalMode_CancelsRequest(t *testing.T) {
	cancelled := false
	m := newModel(defaultConfig()).
		WithLoading(true).
		WithCancel(func() { cancelled = true })

	callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, cancelled)
}

// --- WindowSize ---

func TestUpdate_WindowSize_Stored(t *testing.T) {
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 200, Height: 50})
	assert.Equal(t, 200, m.Width())
	assert.Equal(t, 50, m.Height())
}

func TestUpdate_CollectionPrompt_AddRequest_RequiresSelection(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	assert.Equal(
		t,
		tui.NormalMode,
		m.Mode(),
		"pressing 'a' with no collection selected must not enter prompt mode",
	)
	assert.Equal(t, "Select a collection first", m.StatusErr())
	assert.Contains(
		t,
		m.View(),
		"Select a collection first",
		"error must appear in the status bar",
	)
}

func TestUpdate_CollectionPrompt_Add_ClearsStaleStatusFromAddRequest(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// 'a' with no collection sets a status error.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	require.Equal(t, "Select a collection first", m.StatusErr())

	// 'A' must open the add-collection prompt without carrying that error over.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode())
	assert.Equal(t, tui.PromptAdd, m.PromptMode())
	assert.Empty(t, m.StatusErr(), "stale 'a' error must not appear in the new prompt")
	assert.NotContains(t, m.View(), "Select a collection first")
	assert.Contains(t, m.View(), "New Collection")
}

func TestUpdate_CollectionPrompt_Add_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})

	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'A' must enter collection prompt mode",
	)
	assert.Equal(t, tui.PromptAdd, m.PromptMode(), "prompt mode must be PromptAdd")
}

func TestUpdate_CollectionPrompt_Add_CancelOnEscape(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	assert.Equal(t, tui.CollectionPromptMode, m.Mode())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel back to normal mode")
}

func TestUpdate_CollectionPrompt_Add_EmptyNameShowsError(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"empty name must stay in prompt so user can retry",
	)
	assert.NotEmpty(t, m.StatusErr(), "must show error for empty name")
}

func TestUpdate_CollectionPrompt_AddRequest_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'a' with a collection selected must enter request prompt mode",
	)
	assert.Equal(t, tui.PromptAddRequest, m.PromptMode(), "prompt mode must be PromptAddRequest")
}

// --- Rename collection ---

func TestUpdate_CollectionPrompt_Rename_RequiresSelection(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	assert.Equal(
		t,
		tui.NormalMode,
		m.Mode(),
		"pressing 'r' with no collection selected must not enter prompt mode",
	)
	assert.NotEmpty(t, m.StatusErr(), "must show error when no collection selected")
}

func TestUpdate_CollectionPrompt_Rename_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'r' with a collection selected must enter prompt mode",
	)
	assert.Equal(t, tui.PromptRename, m.PromptMode(), "prompt mode must be PromptRename")
}

func TestUpdate_CollectionPrompt_Rename_CancelOnEscape(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel rename prompt")
}

// --- Delete collection ---

func TestUpdate_CollectionPrompt_Delete_RequiresSelection(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	assert.Equal(
		t,
		tui.NormalMode,
		m.Mode(),
		"pressing 'd' with no collection selected must not enter prompt mode",
	)
	assert.NotEmpty(t, m.StatusErr(), "must show error when no collection selected")
}

func TestUpdate_CollectionPrompt_Delete_EntersPromptMode(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'd' with a collection selected must enter prompt mode",
	)
	assert.Equal(t, tui.PromptDeleteTiny, m.PromptMode(), "prompt mode must be PromptDeleteTiny")
}

func TestUpdate_CollectionPrompt_Delete_CancelOnEscape(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel delete prompt")
}

func TestUpdate_CollectionPrompt_Delete_UsesConfiguredConfirmKey(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.SidebarDelete = "x"
	m := newModel(cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())

	assert.Contains(t, m.View(), "[x] confirm")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "configured delete key must confirm the tiny prompt")
	assert.NotEmpty(
		t,
		m.StatusErr(),
		"without a writer the prompt should still attempt deletion and surface an error",
	)
}

func TestUpdate_CollectionPrompt_UsesConfiguredConfirmAndCancelBindings(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keybindings.ImportConfirm = "x"
	cfg.Keybindings.ImportCancel = "c"
	m := newModel(cfg)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = m.WithFocus(tui.SidebarPane)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode())
	assert.Contains(t, m.View(), "[x] confirm")
	assert.Contains(t, m.View(), "[c] cancel")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "configured cancel key must close the prompt")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"configured confirm key must trigger prompt submission",
	)
	assert.NotEmpty(
		t,
		m.StatusErr(),
		"confirming an empty prompt should surface the validation error",
	)
}

func TestUpdate_CollectionPrompt_Delete_TypingNoShowsError(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Equal(t, tui.CollectionPromptMode, m.Mode(), "pressing 'd' must enter prompt mode")

	// In the tiny prompt, pressing any key other than 'd' just keeps the prompt open
	// (the text input still receives the characters, but Enter is a no-op).
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing Enter with non-'d' must stay in prompt mode",
	)
	assert.Empty(t, m.StatusErr(), "no error shown for tiny prompt")

	// Esc cancels the prompt and returns to normal mode.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Esc must cancel delete prompt")
}

func TestUpdate_PromptDeleteConfirm_TypingNo_Cancels(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")
	m = m.WithPromptInputValue("no")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
	assert.NotEmpty(t, model.StatusErr(), "should show error")
	assert.Contains(t, model.StatusErr(), "cancelled")
}

func TestUpdate_PromptDeleteConfirm_EmptyInput_Cancels(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")
	m = m.WithPromptInputValue("")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
	assert.NotEmpty(t, model.StatusErr(), "should show error")
	assert.Contains(t, model.StatusErr(), "cancelled")
}

func TestUpdate_PromptDeleteConfirm_CaseSensitive_Fails(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")
	m = m.WithPromptInputValue("YES")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
	assert.NotEmpty(t, model.StatusErr(), "should show error")
	assert.Contains(t, model.StatusErr(), "cancelled")
}

func TestUpdate_PromptDeleteConfirm_Esc_Cancels(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.CollectionPromptMode).
		WithPromptMode(tui.PromptDeleteConfirm).
		WithPromptTargetID("col-1")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, tui.NormalMode, model.Mode(), "should return to normal mode")
}

func TestUpdate_HelpScroll_UpMovesCursorBeforeViewport(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 28})
	m = m.WithMode(tui.HelpMode)

	for i := 0; i < 25; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}

	require.Greater(t, m.HelpScrollOffset(), 0, "setup should scroll the help viewport down")
	offsetBefore := m.HelpScrollOffset()
	cursorBefore := m.HelpCursor()

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	assert.Equal(t, cursorBefore-1, m.HelpCursor(), "up should move the cursor first")
	assert.Equal(
		t,
		offsetBefore,
		m.HelpScrollOffset(),
		"viewport should stay stable while the cursor is still inside the visible comfort zone",
	)
}

func TestUpdate_HelpScroll_ViewportEventuallyMovesWhenCursorKeepsGoingUp(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 28})
	m = m.WithMode(tui.HelpMode)

	for i := 0; i < 25; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}

	require.Greater(t, m.HelpScrollOffset(), 0, "setup should scroll the help viewport down")
	offsetBefore := m.HelpScrollOffset()

	for i := 0; i < 20; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	}

	assert.Less(
		t,
		m.HelpScrollOffset(),
		offsetBefore,
		"viewport should scroll upward once the cursor leaves the upper comfort zone",
	)
}

func TestUpdate_SchedulePrompt_SavesPendingRunWithFixedClock(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	cfg := config.Default("")
	scheduler := &fakeScheduler{}
	m := tui.New(tui.Deps{
		Config:    cfg,
		Scheduler: scheduler,
		Resolver:  keybindings.NewResolver(cfg.Keybindings),
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	})
	m = m.WithFocus(tui.RequestPane).WithActiveRequest(&domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	require.Equal(t, tui.ScheduleMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10m")})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	msg := cmd()
	model = callUpdate(t, model, msg)

	require.Len(t, scheduler.runs, 1)
	assert.Equal(t, "req-1", scheduler.runs[0].RequestID)
	assert.Equal(t, now.Add(10*time.Minute), scheduler.runs[0].RunAt)
	assert.Equal(t, domain.ScheduledRunPending, scheduler.runs[0].Status)
	assert.Contains(t, model.StatusSuccess(), "Scheduled for")
}

func TestUpdate_ScheduleTimer_StaleWakeIsIgnored(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	}
	scheduler := &fakeScheduler{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests:    map[string]*domain.Request{req.ID: req},
		runs: []*domain.ScheduledRun{{
			ID:        "run-1",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Minute),
			Status:    domain.ScheduledRunPending,
		}},
	}
	executor := &recordingExecutor{}
	m := tui.New(tui.Deps{
		Config:    config.Default(""),
		Lister:    scheduler,
		Scheduler: scheduler,
		Reader:    scheduler,
		Executor:  executor,
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	}).WithScheduleTimerSeq(2)

	_, cmd := m.Update(tui.ScheduledRunWakeMsg(1))

	require.Nil(t, cmd)
	assert.Equal(t, 0, executor.calls)
	assert.Equal(t, domain.ScheduledRunPending, scheduler.runs[0].Status)
}

func TestUpdate_ScheduleTimer_MissedRunShowsRetryWarning(t *testing.T) {
	m := tui.New(tui.Deps{Config: config.Default(""), Ctx: context.Background()}).
		WithScheduleTimerSeq(4)

	updated, cmd := m.Update(tui.ScheduledRunMissedMsg(4, "List Payments"))
	require.NotNil(t, cmd)
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	assert.Contains(
		t,
		model.StatusErr(),
		`We missed executing your scheduled request "List Payments"`,
	)
}

func TestUpdate_ScheduleTimer_ExecutesDueRunAndShowsBackgroundStatus(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	}
	scheduler := &fakeScheduler{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests:    map[string]*domain.Request{req.ID: req},
		runs: []*domain.ScheduledRun{{
			ID:        "run-1",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Minute),
			Status:    domain.ScheduledRunPending,
		}},
	}
	executor := &recordingExecutor{}
	m := tui.New(tui.Deps{
		Config:    config.Default(""),
		Lister:    scheduler,
		Scheduler: scheduler,
		Reader:    scheduler,
		Executor:  executor,
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	}).WithScheduleTimerSeq(7).WithActiveRequest(req)

	updated, cmd := m.Update(tui.ScheduledRunWakeMsg(7))
	require.NotNil(t, cmd)
	msg := cmd()
	model, ok := updated.(tui.Model)
	require.True(t, ok)
	updated, _ = model.Update(msg)
	model, ok = updated.(tui.Model)
	require.True(t, ok)

	assert.Equal(t, 1, executor.calls)
	require.Len(t, executor.reqs, 1)
	assert.Equal(t, req.ID, executor.reqs[0].ID)
	assert.Equal(t, domain.ScheduledRunCompleted, scheduler.runs[0].Status)
	assert.Contains(t, model.StatusSuccess(), `Sent "Payments / List Payments" in the background`)
	require.NotNil(t, model.Response())
	assert.Equal(t, 200, model.Response().StatusCode)
}

func TestUpdate_ScheduleTimer_BackgroundFailureIncludesStatusSaveError(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)
	req := &domain.Request{
		ID:           "req-1",
		CollectionID: "col-1",
		Name:         "List Payments",
		Method:       "GET",
		URL:          "https://example.test/payments",
	}
	scheduler := &fakeScheduler{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests:    map[string]*domain.Request{},
		runs: []*domain.ScheduledRun{{
			ID:        "run-1",
			RequestID: req.ID,
			RunAt:     now.Add(-time.Minute),
			Status:    domain.ScheduledRunPending,
		}},
		saveErrByID: map[string]error{"run-1": assert.AnError},
	}
	m := tui.New(tui.Deps{
		Config:    config.Default(""),
		Lister:    scheduler,
		Scheduler: scheduler,
		Reader:    scheduler,
		Executor:  &recordingExecutor{},
		Ctx:       context.Background(),
		Now:       func() time.Time { return now },
	}).WithScheduleTimerSeq(0)

	updated, cmd := m.Update(tui.ScheduledRunWakeMsg(0))
	require.NotNil(t, cmd)
	model, ok := updated.(tui.Model)
	require.True(t, ok)

	msg := cmd()
	model = callUpdate(t, model, msg)

	require.NotEmpty(t, model.StatusErr())
	assert.Contains(t, model.StatusErr(), "failed")
	assert.Contains(t, model.StatusErr(), assert.AnError.Error())
	assert.Equal(t, 1, scheduler.saveCalls["run-1"])
}

func TestUpdate_EscFromImportMode_ClearsEditingURL(t *testing.T) {
	m := newTestModel()
	// Simulate state after triggerCurlImport: importMode active, editingURL still true.
	m = m.WithMode(tui.ImportMode).WithActiveField(tui.URLField)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(
		t,
		tui.NormalMode,
		m.Mode(),
		"mode must return to normal after Esc from importMode",
	)
	assert.Equal(
		t, tui.NoneField, m.ActiveField(),
		"activeField must be noneField after Esc from importMode",
	)
}

func TestUpdate_EscFromImportMode_AllowsGlobalKeys(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.ImportMode).WithActiveField(tui.URLField)
	// Escape import mode
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	// Now pressing '1' must switch to sidebar pane, not type into URL.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	assert.Equal(
		t,
		tui.SidebarPane,
		m.Focus(),
		"pressing '1' after escape must focus sidebar, not type into URL",
	)
}

// --- BUG-003: double "invalid URL:" prefix in validation error message ---

func TestUpdate_InvalidURLError_NoDuplicatePrefix(t *testing.T) {
	m := newTestModel()
	m = m.WithActiveRequest(&domain.Request{ID: "req-1", Method: "GET", URL: "bad"})
	// Executor now wraps once: "exec: build request: <buildHTTPRequest error>".
	// buildHTTPRequest wraps ErrInvalidURL → "invalid URL: scheme..."
	// Combined: "exec: build request: invalid URL: scheme..."
	// cleanError strips "exec: build request: " → "invalid URL: scheme..." (single prefix).
	inner := fmt.Errorf(
		"%w: scheme %q is not allowed (must be http or https)",
		exec.ErrInvalidURL,
		"",
	)
	wrapped := fmt.Errorf("exec: build request: %w", inner) // fixed single-wrap

	assert.True(t, errors.Is(wrapped, exec.ErrInvalidURL))

	m = callUpdate(t, m, tui.HttpErrMsg(wrapped))

	got := m.ValidationErr()
	assert.NotContains(t, got, "invalid URL: invalid URL:",
		"error must not contain duplicated prefix; got: %q", got)
	assert.Contains(t, got, "invalid URL:", "error must retain the single prefix; got: %q", got)
}

// --- BUG-004: stub keys show no user feedback ---

func TestUpdate_SidebarAddRequest_OpensPrompt(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.SidebarPane)
	m = m.WithCollections([]*domain.Collection{{ID: "col-1", Name: "My Col"}})
	m = m.WithColCursor(0)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	// BUG-004: previously pressed 'a' was a silent stub. Now it opens the
	// request prompt modal for the selected collection.
	assert.Equal(
		t,
		tui.CollectionPromptMode,
		m.Mode(),
		"pressing 'a' must open request prompt mode",
	)
	assert.Equal(t, tui.PromptAddRequest, m.PromptMode())
}

func TestUpdate_EditBodyWithoutActiveRequest_ShowsFeedback(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.NotEmpty(
		t,
		m.StatusErr(),
		"pressing 'b' with no active request must show a status message",
	)
}

func TestUpdate_EditHeadersWithoutActiveRequest_ShowsFeedback(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.NotEmpty(
		t,
		m.StatusErr(),
		"pressing 'h' with no active request must show a status message",
	)
}

// --- BUG-008: "No results." shown before any search is performed ---

func TestUpdate_SearchModal_BeforeSearch_NotSearchedYet(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, tui.SearchMode, m.Mode())
	assert.False(
		t,
		m.Searched(),
		"Searched must be false when modal first opens, before any result arrives",
	)
}

func TestUpdate_SearchModal_AfterResultsMsg_MarkedSearched(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = callUpdate(t, m, tui.SearchResultsMsg(nil))
	assert.True(t, m.Searched(), "Searched must be true after first SearchResultsMsg received")
}

func TestUpdate_SearchModal_ReopenClearsStaleQuery(t *testing.T) {
	m := newTestModel().WithSearchInputValue("post")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, tui.SearchMode, m.Mode())
	assert.Equal(t, "", m.SearchInputValue(), "search input must be cleared when search opens")
}

// --- BUG-009: pressing 'q' in help mode closes help instead of quitting ---

func TestUpdate_QuitInHelpMode_Quits(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.HelpMode)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	require.NotNil(t, cmd, "pressing 'q' in help mode must return a non-nil command")
	msg := cmd() // invoke the Cmd to get the message
	assert.IsType(t, tea.QuitMsg{}, msg, "pressing 'q' in help mode must return tea.Quit")
}

// --- BUG-007: old streamed response temp file not cleaned up on new response ---

func TestUpdate_HttpResponse_CleansUpOldTempFile(t *testing.T) {
	// Create a real temp file to simulate a previously streamed response.
	f, err := os.CreateTemp("", "quark-test-*.tmp")
	require.NoError(t, err)
	tmpPath := f.Name()
	f.Close()

	oldResult := &exec.ExecuteResult{TempPath: tmpPath}
	m := newTestModel().WithResponse(oldResult)

	// Send a new (non-streamed) response — should clean up the old temp file.
	newResult := &exec.ExecuteResult{StatusCode: 200, Body: []byte("hello")}
	_ = callUpdate(t, m, tui.HttpResponseMsg(newResult))

	_, statErr := os.Stat(tmpPath)
	assert.True(
		t,
		os.IsNotExist(statErr),
		"old temp file must be removed when response is replaced; still exists: %s",
		tmpPath,
	)
	// Cleanup in case test fails.
	_ = os.Remove(tmpPath)
}

func TestUpdate_HttpResponse_FirstResponseNoCleanupNeeded(t *testing.T) {
	// No prior response — should not panic.
	m := newTestModel()
	newResult := &exec.ExecuteResult{StatusCode: 200, Body: []byte("hello")}
	m = callUpdate(t, m, tui.HttpResponseMsg(newResult))
	assert.NotNil(t, m.Response(), "response must be stored")
}

// --- BUG-008: searchCancel not called when Esc dismisses search mode ---

func TestUpdate_EscFromSearchMode_CallsSearchCancel(t *testing.T) {
	m := newTestModel().WithMode(tui.SearchMode)
	m = m.WithSearchInputValue("post")

	cancelled := false
	m = m.WithSearchCancel(func() { cancelled = true })

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(t, tui.NormalMode, m.Mode(), "mode must return to normal")
	assert.True(t, cancelled, "searchCancel must be called when Esc dismisses search mode")
	assert.Nil(t, m.SearchCancel(), "searchCancel must be nil after Esc")
	assert.Equal(
		t,
		"",
		m.SearchInputValue(),
		"search input must be cleared when Esc dismisses search",
	)
}

func TestUpdate_EscFromSearchMode_NilCancelOK(t *testing.T) {
	// No search in flight — must not panic.
	m := newTestModel().WithMode(tui.SearchMode).WithSearchInputValue("post")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(
		t,
		tui.NormalMode,
		m.Mode(),
		"mode must return to normal even with nil searchCancel",
	)
	assert.Equal(
		t,
		"",
		m.SearchInputValue(),
		"search input must be cleared even when no search is in flight",
	)
}

func TestUpdate_ResolverRemapHelpAndQuit(t *testing.T) {
	custom := keybindings.DefaultKeybindings()
	custom.Help = "T"
	custom.Quit = "Q"

	m := tui.New(tui.Deps{
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   config.Default(""),
		Ctx:      context.Background(),
		Resolver: keybindings.NewResolver(custom),
	})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// T should open help
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	assert.Equal(t, tui.HelpMode, m.Mode(), "T must open help mode")

	// ? should NOT open help (it's unmapped)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // close help
	require.Equal(t, tui.NormalMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "? must NOT open help mode when help is T")

	// In help mode, Q should quit
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	require.Equal(t, tui.HelpMode, m.Mode())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	require.NotNil(t, cmd, "Q must quit from help mode")

	// q should NOT quit from help mode
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.Equal(t, tui.HelpMode, m.Mode(), "q must NOT quit help mode when quit is Q")
}

// TestUpdate_ResolverSingleKeybindingHelp verifies that when help is set to !,
// both ? and T are dead, and only ! works.

func TestUpdate_ResolverSingleKeybindingHelp(t *testing.T) {
	custom := keybindings.DefaultKeybindings()
	custom.Help = "!"
	custom.Quit = "Q"

	m := tui.New(tui.Deps{
		Searcher: &search.Searcher{},
		Importer: curl.NewImporter(),
		Config:   config.Default(""),
		Ctx:      context.Background(),
		Resolver: keybindings.NewResolver(custom),
	})
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// ? should NOT work
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, tui.NormalMode, m.Mode(), "? must be dead when help is !")

	// ! should open help
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	assert.Equal(t, tui.HelpMode, m.Mode(), "! must open help mode")
}
