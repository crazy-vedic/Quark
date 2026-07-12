//go:build e2e

package tui_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

const (
	e2eMouseWidth  = 120
	e2eMouseHeight = 40
)

func resizedMouseModel(t *testing.T) tui.Model {
	t.Helper()
	m := newE2EModel(t, setupStore(t), &mockExecutor{})
	return resize(t, m, e2eMouseWidth, e2eMouseHeight)
}

func TestE2E_ClickResponseTabSwitchesView(t *testing.T) {
	ex := &domain.Execution{
		ID: "ex-1", RequestID: "req-1", StatusCode: 200,
		ResponseBody: `{"ok":true}`, ResponseHeaders: `{"X-Test":["1"]}`,
	}
	m := resizedMouseModel(t).
		WithExecutions([]*domain.Execution{ex}).
		WithExecCursor(0).
		WithResponseTab(tui.BodyTab)

	x, y, ok := m.ResponseTabClickPos(tui.HeadersTab)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	assert.Equal(t, tui.ResponsePane, m.Focus())
	assert.Equal(t, tui.HeadersTab, m.ResponseTab())
	assertViewContains(t, m, "X-Test")
}

func TestE2E_ResponseWheelCyclesHistory(t *testing.T) {
	execs := []*domain.Execution{
		{
			ID:              "ex-0",
			RequestID:       "req-1",
			StatusCode:      200,
			ResponseBody:    "newest",
			ResponseHeaders: `{}`,
		},
		{
			ID:              "ex-1",
			RequestID:       "req-1",
			StatusCode:      200,
			ResponseBody:    "older",
			ResponseHeaders: `{}`,
		},
	}
	m := resizedMouseModel(t).
		WithExecutions(execs).
		WithExecCursor(0).
		WithFocus(tui.ResponsePane)

	x, y, ok := m.ResponsePaneWheelPos()
	require.True(t, ok)
	m = callUpdate(t, m, wheelDown(x, y))
	assert.Equal(t, 1, m.ExecCursor())
	m = callUpdate(t, m, wheelUp(x, y))
	assert.Equal(t, 0, m.ExecCursor())
}

func TestE2E_ClickFocusesSidebarPane(t *testing.T) {
	m := resizedMouseModel(t).WithFocus(tui.RequestPane)

	m = callUpdate(t, m, click(5, 1))

	assert.Equal(t, tui.SidebarPane, m.Focus())
}

func TestE2E_ClickFocusesRequestPane(t *testing.T) {
	m := resizedMouseModel(t).WithFocus(tui.SidebarPane)

	m = callUpdate(t, m, click(50, 5))

	assert.Equal(t, tui.RequestPane, m.Focus())
}

func TestE2E_ClickFocusesResponsePane(t *testing.T) {
	m := resizedMouseModel(t).WithFocus(tui.SidebarPane)

	m = callUpdate(t, m, click(50, 25))

	assert.Equal(t, tui.ResponsePane, m.Focus())
}

func TestE2E_MouseClickIgnoredBeforeResizeOrOutsideLayout(t *testing.T) {
	m := newE2EModel(t, setupStore(t), &mockExecutor{}).WithFocus(tui.RequestPane)
	require.Equal(t, 0, m.Width())
	require.Equal(t, 0, m.Height())

	m = callUpdate(t, m, click(5, 1))
	assert.Equal(t, tui.RequestPane, m.Focus())

	m = callUpdate(t, m, click(-1, -1))
	assert.Equal(t, tui.RequestPane, m.Focus())

	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m = callUpdate(t, m, click(500, 500))
	assert.Equal(t, tui.RequestPane, m.Focus())

	m = callUpdate(t, m, click(10, e2eMouseHeight-1))
	assert.Equal(t, tui.RequestPane, m.Focus())
}

func TestE2E_MouseIgnoredInOverlayModesInitially(t *testing.T) {
	t.Run("search mode", func(t *testing.T) {
		m := resizedMouseModel(t)
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		require.Equal(t, tui.SearchMode, m.Mode())

		m = callUpdate(t, m, click(5, 1))
		assert.Equal(t, tui.SearchMode, m.Mode())
	})

	t.Run("help mode", func(t *testing.T) {
		m := resizedMouseModel(t)
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		require.Equal(t, tui.HelpMode, m.Mode())

		m = callUpdate(t, m, click(50, 5))
		assert.Equal(t, tui.HelpMode, m.Mode())
	})
}

func TestE2E_KeyboardFocusStillWorksAfterMouse(t *testing.T) {
	m := resizedMouseModel(t)

	m = callUpdate(t, m, click(50, 5))
	assert.Equal(t, tui.RequestPane, m.Focus())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

func sidebarE2EModel(t *testing.T, cols ...*domain.Collection) (tui.Model, []*domain.Collection) {
	t.Helper()
	if len(cols) == 0 {
		cols = []*domain.Collection{
			{ID: "col-1", Name: "Alpha"},
			{ID: "col-2", Name: "Beta"},
		}
	}
	st := setupStore(t, cols...)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(cols))
	return m, cols
}

func TestE2E_ClickCollectionRowSelectsCollection(t *testing.T) {
	m, _ := sidebarE2EModel(t)
	x, y, ok := m.SidebarCollectionClickPos(1)
	require.True(t, ok)

	m = callUpdate(t, m, click(x, y))
	assert.Equal(t, 1, m.ColCursor())
	assert.Equal(t, -1, m.ReqCursor())
	assert.Equal(t, tui.SidebarPane, m.Focus())
}

func TestE2E_ClickCollectionDisclosureExpandsAndLoadsRequests(t *testing.T) {
	col1 := &domain.Collection{ID: "col-1", Name: "Alpha"}
	col2 := &domain.Collection{ID: "col-2", Name: "Beta"}
	st := setupStore(t, col1, col2)
	seedRequests(
		t,
		st,
		col2.ID,
		&domain.Request{
			ID:     "req-1",
			Name:   "Get Item",
			Method: "GET",
			URL:    "https://example.test/item",
		},
	)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m, cmd := callUpdateWithCmd(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col1, col2}))
	require.NotNil(t, cmd)
	m = executeCmdUpdate(t, m, cmd) // auto-load for first collection

	x, y, ok := m.SidebarCollectionDisclosurePos(1)
	require.True(t, ok)

	m, cmd = callUpdateWithCmd(t, m, click(x, y))
	require.NotNil(t, cmd)
	m = callUpdate(t, m, runCmd(t, cmd))

	assert.True(t, m.IsExpanded(col2.ID))
	assertViewContains(t, m, "Get Item")
}

func TestE2E_ClickCollectionDisclosureCollapses(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "Alpha"}
	st := setupStore(t, col)
	seedRequests(
		t,
		st,
		col.ID,
		&domain.Request{
			ID:     "req-1",
			Name:   "Get Item",
			Method: "GET",
			URL:    "https://example.test/item",
		},
	)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m, cmd := callUpdateWithCmd(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = executeCmdUpdate(t, m, cmd)
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{
		{ID: "req-1", Name: "Get Item", Method: "GET", URL: "https://example.test/item"},
	}))
	assertViewContains(t, m, "Get Item")

	x, y, ok := m.SidebarCollectionClickPos(0)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))

	assert.False(t, m.IsExpanded(col.ID))
	assert.Equal(t, -1, m.ReqCursor())
	assertViewNotContains(t, m, "Get Item")
}

func TestE2E_ClickSelectedCollectionRowTogglesExpand(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "Alpha"}
	st := setupStore(t, col)
	seedRequests(
		t,
		st,
		col.ID,
		&domain.Request{
			ID:     "req-1",
			Name:   "Get Item",
			Method: "GET",
			URL:    "https://example.test/item",
		},
	)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m, cmd := callUpdateWithCmd(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = executeCmdUpdate(t, m, cmd)
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{
		{ID: "req-1", Name: "Get Item", Method: "GET", URL: "https://example.test/item"},
	}))
	assertViewContains(t, m, "Get Item")

	x, y, ok := m.SidebarCollectionClickPos(0)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	assert.False(t, m.IsExpanded(col.ID))
	assertViewNotContains(t, m, "Get Item")

	m = callUpdate(t, m, click(x, y))
	assert.True(t, m.IsExpanded(col.ID))
}

func TestE2E_ClickRequestRowSelectsAndOpensRequest(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Get JSON", Method: "GET", URL: "https://example.test/json",
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m, cmd := callUpdateWithCmd(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = executeCmdUpdate(t, m, cmd)
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))

	x, y, ok := m.SidebarRequestClickPos(0, 0)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))

	assert.Equal(t, 0, m.ColCursor())
	assert.Equal(t, 0, m.ReqCursor())
	assert.Equal(t, tui.RequestPane, m.Focus())
	require.NotNil(t, m.ActiveRequest())
	assert.Equal(t, "Get JSON", m.ActiveRequest().Name)
	assert.Equal(t, "https://example.test/json", m.URLValue())
	assertViewContains(t, m, "https://example.test/json")
}

func TestE2E_ClickRequestThenKeyboardNavigationContinuesFromSameRow(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	reqs := []*domain.Request{
		{ID: "req-1", Name: "First", Method: "GET", URL: "https://example.test/1"},
		{ID: "req-2", Name: "Second", Method: "GET", URL: "https://example.test/2"},
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, reqs...)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m, cmd := callUpdateWithCmd(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = executeCmdUpdate(t, m, cmd)
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, reqs))

	x, y, ok := m.SidebarRequestClickPos(0, 0)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}}) // focus sidebar
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	assert.Equal(t, 0, m.ColCursor())
	assert.Equal(t, 1, m.ReqCursor())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, m.ColCursor())
	assert.Equal(t, 0, m.ReqCursor())
}

func TestE2E_SidebarWheelScrollsLongTree(t *testing.T) {
	cols := make([]*domain.Collection, 0, 30)
	for i := 0; i < 30; i++ {
		cols = append(cols, &domain.Collection{
			ID:   fmt.Sprintf("col-%d", i),
			Name: fmt.Sprintf("Collection %02d", i),
		})
	}
	st := setupStore(t, cols...)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, 20)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(cols))

	m = callUpdate(t, m, wheelDown(5, 5))
	assert.Equal(t, 1, m.ColCursor())

	for i := 0; i < 25; i++ {
		m = callUpdate(t, m, wheelDown(5, 5))
	}
	assert.Greater(t, m.ColCursor(), 0)
	assert.Less(t, m.ColCursor(), len(cols))
	assert.Greater(t, m.SidebarOffset(), 0)

	prevCursor := m.ColCursor()
	m = callUpdate(t, m, wheelUp(5, 5))
	assert.Equal(t, prevCursor-1, m.ColCursor())
}

func TestE2E_SidebarMouseNarrowWidth(t *testing.T) {
	m, _ := sidebarE2EModel(t)
	m = resize(t, m, 70, e2eMouseHeight)

	x, y, ok := m.SidebarCollectionClickPos(1)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	assert.Equal(t, 1, m.ColCursor())
}

func TestE2E_SidebarClickMoreBelowScrolls(t *testing.T) {
	cols := make([]*domain.Collection, 0, 40)
	for i := 0; i < 40; i++ {
		cols = append(cols, &domain.Collection{
			ID:   fmt.Sprintf("col-%d", i),
			Name: fmt.Sprintf("Collection %02d", i),
		})
	}
	st := setupStore(t, cols...)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, 18)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(cols))

	before := m.SidebarOffset()
	x, y, ok := m.SidebarMoreBelowPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	assert.Greater(t, m.SidebarOffset(), before)
}

func TestE2E_SidebarClickEmptyTreeNoPanic(t *testing.T) {
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg(nil))

	m = callUpdate(t, m, click(3, 3))
	assert.Equal(t, tui.SidebarPane, m.Focus())
	assert.Equal(t, 0, m.ColCursor())
}

func requestE2EModel(t *testing.T) tui.Model {
	t.Helper()
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Get JSON", Method: "GET", URL: "https://example.test/json",
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m, cmd := callUpdateWithCmd(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = executeCmdUpdate(t, m, cmd)
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))

	x, y, ok := m.SidebarRequestClickPos(0, 0)
	require.True(t, ok)
	return callUpdate(t, m, click(x, y))
}

func TestE2E_ClickMethodBadgeCyclesMethod(t *testing.T) {
	m := requestE2EModel(t)
	x, y, ok := m.RequestMethodBadgeClickPos()
	require.True(t, ok)

	want := []string{"POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "GET"}
	for _, method := range want {
		m = callUpdate(t, m, click(x, y))
		assert.Equal(t, method, m.Method())
	}
}

func TestE2E_ClickURLLineBeginsURLEdit(t *testing.T) {
	m := requestE2EModel(t)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)

	m = callUpdate(t, m, click(x, y))
	assert.Equal(t, tui.URLField, m.ActiveField())
	assert.Equal(t, tui.RequestPane, m.Focus())
	assertViewContains(t, m, "https://example.test/json")
}

func TestE2E_ClickSendButtonExecutesRequest(t *testing.T) {
	m := requestE2EModel(t)
	x, y, ok := m.RequestSendButtonClickPos()
	require.True(t, ok)

	m, cmd := callUpdateWithCmd(t, m, click(x, y))
	require.NotNil(t, cmd)
	assert.True(t, m.Loading())
	assertViewContains(t, m, "Sending…")

	m = callUpdate(t, m, runCmd(t, cmd))
	require.NotNil(t, m.Response())
	assert.Equal(t, 200, m.Response().StatusCode)
	assertViewContains(t, m, "200 OK")
}

func TestE2E_ClickSendButtonShowsValidationForInvalidURL(t *testing.T) {
	m := requestE2EModel(t)
	m = m.WithURLValue("not-a-url")

	x, y, ok := m.RequestSendButtonClickPos()
	require.True(t, ok)
	m, cmd := callUpdateWithCmd(t, m, click(x, y))
	require.NotNil(t, cmd)

	m = callUpdate(t, m, tui.HttpErrMsg(fmt.Errorf("%w", exec.ErrInvalidURL)))
	assert.False(t, m.Loading())
	assert.NotEmpty(t, m.ValidationErr())
	assertViewContains(t, m, "invalid URL")
}

type countingExecutor struct {
	calls int
}

func (c *countingExecutor) Execute(
	_ context.Context,
	_ *domain.Request,
) (*exec.ExecuteResult, error) {
	c.calls++
	return &exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       []byte("OK"),
		Duration:   1 * time.Millisecond,
		Size:       2,
	}, nil
}

func TestE2E_ClickSendWhileLoadingDoesNotDuplicate(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Get JSON", Method: "GET", URL: "https://example.test/json",
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	ex := &countingExecutor{}
	m := newE2EModel(t, st, ex)
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m, cmd := callUpdateWithCmd(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = executeCmdUpdate(t, m, cmd)
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	x, y, ok := m.SidebarRequestClickPos(0, 0)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))

	sx, sy, ok := m.RequestSendButtonClickPos()
	require.True(t, ok)

	m, cmd = callUpdateWithCmd(t, m, click(sx, sy))
	require.NotNil(t, cmd)
	assert.True(t, m.Loading())

	_, cmd2 := callUpdateWithCmd(t, m, click(sx, sy))
	assert.Nil(t, cmd2)

	m = callUpdate(t, m, runCmd(t, cmd))
	assert.Equal(t, 1, ex.calls)
}

func TestE2E_ClickRequestPaneChromeDoesNotAccidentallyEdit(t *testing.T) {
	m := requestE2EModel(t)
	x, y, ok := m.RequestPaneContentClickPos()
	require.True(t, ok)

	m = callUpdate(t, m, click(x, y))
	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, tui.RequestPane, m.Focus())
}

func TestE2E_KeyboardSendStillWorksAfterMouseSupport(t *testing.T) {
	m := requestE2EModel(t)

	x, y, ok := m.RequestMethodBadgeClickPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	require.NotNil(t, cmd)
	assert.True(t, m.Loading())
}

func TestE2E_ClickURLTextMovesCursorBeforeTyping(t *testing.T) {
	m := requestE2EModel(t).WithURLValue("https://example.test/path")
	x, y, ok := m.RequestURLTextClickPosAtColumn(8)
	require.True(t, ok)

	m = callUpdate(t, m, click(x, y))
	assert.Equal(t, tui.URLField, m.ActiveField())
	assert.Equal(t, 8, m.URLCursorPosition())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	assert.Equal(t, "https://Xexample.test/path", m.URLValue())
}

func TestE2E_ClickURLStartAndEndPositions(t *testing.T) {
	m := requestE2EModel(t).WithURLValue("https://example.test/path")

	sx, sy, ok := m.RequestURLTextClickPosAtColumn(0)
	require.True(t, ok)
	m = callUpdate(t, m, click(sx, sy))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}})
	assert.True(t, strings.HasPrefix(m.URLValue(), "<"))

	ex, ey, ok := m.RequestURLTextClickPosAtColumn(500)
	require.True(t, ok)
	m = callUpdate(t, m, click(ex, ey))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'>'}})
	assert.True(t, strings.HasSuffix(m.URLValue(), ">"))
}

func TestE2E_ClickURLWithWideCharactersDoesNotPanic(t *testing.T) {
	m := requestE2EModel(t).WithURLValue("a世b")
	x, y, ok := m.RequestURLTextClickPosAtColumn(2)
	require.True(t, ok)

	assert.NotPanics(t, func() {
		_ = callUpdate(t, m, click(x, y))
	})
}

func TestE2E_ClickHeaderRowOpensEditor(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.test",
		Headers: `{"A-Key":"a","B-Key":"b"}`,
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	require.Equal(t, tui.HeadersField, m.ActiveField())
	require.False(t, m.HeaderEditing())
	require.Len(t, m.HeaderPairs(), 2)

	targetKey := m.HeaderPairs()[1].Key
	x, y, ok := m.HeaderListRowClickPos(1)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))

	assert.Equal(t, 1, m.HeaderCursor())
	assert.True(t, m.HeaderEditing())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Contains(t, m.ActiveRequest().Headers, targetKey)
}

func TestE2E_ClickHeaderKeyAndValueInputsFocusCorrectField(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Create", Method: "POST", URL: "https://example.test",
		Headers: `{"X-Test":"old"}`,
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	vx, vy, ok := m.HeaderValueInputClickPosAtColumn(20)
	require.True(t, ok)
	m = callUpdate(t, m, click(vx, vy))
	for _, r := range "-value" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.Contains(t, m.ActiveRequest().Headers, `"X-Test"`)
	assert.Contains(t, m.ActiveRequest().Headers, `old-value`)
}

func TestE2E_ClickAuthInputFocusesCorrectField(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Get", Method: "GET", URL: "https://example.test",
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}) // None -> Bearer

	x, y, ok := m.AuthRowClickPos(1) // token row
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	for _, r := range "mouse-token" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		m = executeCmdUpdate(t, m, cmd)
	}

	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Contains(t, m.ActiveRequest().AuthConfig, "mouse-token")
	assertViewContains(t, m, "Auth: Bearer")
}

func TestE2E_ClickBodyTextareaMovesCursorBeforeTyping(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Post", Method: "POST", URL: "https://example.test",
		Body: "line one\nline two\nline three",
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, e2eMouseHeight)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	x, y, ok := m.BodyTextareaClickPos(1, 3)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	assert.Equal(t, "line one\nlin!e two\nline three", m.BodyValue())
}

func TestE2E_BodyTextareaWheelScrollsVisibleContent(t *testing.T) {
	col := &domain.Collection{ID: "col-1", Name: "API"}
	lines := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("body-line-%02d", i))
	}
	req := &domain.Request{
		ID: "req-1", Name: "Post", Method: "POST", URL: "https://example.test",
		Body: strings.Join(lines, "\n"),
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)

	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, e2eMouseWidth, 24)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	before := m.View()
	x, y, ok := m.BodyTextareaClickPos(0, 0)
	require.True(t, ok)
	for i := 0; i < 5; i++ {
		m = callUpdate(t, m, wheelDown(x, y))
	}
	after := m.View()
	assert.NotEqual(t, before, after)
	assertViewContains(t, m, "body-line-")
}

func TestE2E_EditorClickOutsideInputDoesNotLoseUnsavedState(t *testing.T) {
	m := requestE2EModel(t).WithURLValue("https://example.test/path")
	x, y, ok := m.RequestURLTextClickPosAtColumn(8)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	px, py, ok := m.RequestPaneContentClickPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(px, py))
	assert.Equal(t, tui.URLField, m.ActiveField())
	assert.Contains(t, m.URLValue(), "X")
}
