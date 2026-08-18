package tui_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

func resizedMouseUnitModel(t *testing.T) tui.Model {
	t.Helper()
	m := newModel(defaultConfig()).WithFocus(tui.RequestPane).WithActiveField(tui.URLField)
	return callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
}

func TestUpdate_Mouse_ResponseTabClick(t *testing.T) {
	t.Parallel()

	ex := &domain.Execution{
		ID: "ex-1", RequestID: "req-1", StatusCode: 200,
		ResponseBody: `{"ok":true}`, ResponseHeaders: `{}`,
	}
	m := resizedMouseUnitModel(t).
		WithExecutions([]*domain.Execution{ex}).
		WithExecCursor(0).
		WithResponseTab(tui.BodyTab).
		WithFocus(tui.SidebarPane)

	x, y, ok := m.ResponseTabClickPos(tui.HeadersTab)
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.ResponsePane, m.Focus())
	assert.Equal(t, tui.HeadersTab, m.ResponseTab())

	x, y, ok = m.ResponseTabClickPos(tui.RawTab)
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.RawTab, m.ResponseTab())
}

func TestUpdate_Mouse_ResponseWheelCyclesHistory(t *testing.T) {
	t.Parallel()

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
		{
			ID:              "ex-2",
			RequestID:       "req-1",
			StatusCode:      200,
			ResponseBody:    "oldest",
			ResponseHeaders: `{}`,
		},
	}
	m := resizedMouseUnitModel(t).
		WithExecutions(execs).
		WithExecCursor(0).
		WithFocus(tui.ResponsePane)

	x, y, ok := m.ResponsePaneWheelPos()
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})
	assert.Equal(t, 1, m.ExecCursor())

	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
	})
	assert.Equal(t, 0, m.ExecCursor())
}

func TestUpdate_Mouse_ResponseTextWheelDoesNotCycleHistory(t *testing.T) {
	t.Parallel()

	longBody := ""
	for i := 0; i < 80; i++ {
		longBody += fmt.Sprintf("line-%03d\n", i)
	}
	execs := []*domain.Execution{
		{ID: "ex-0", RequestID: "req-1", StatusCode: 200, ResponseBody: longBody, ResponseHeaders: `{}`},
		{ID: "ex-1", RequestID: "req-1", StatusCode: 200, ResponseBody: "older", ResponseHeaders: `{}`},
	}
	m := resizedMouseUnitModel(t).
		WithExecutions(execs).
		WithExecCursor(0).
		WithFocus(tui.ResponsePane)

	before := m.View()
	x, y, ok := m.ResponseTextWheelPos()
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})

	assert.Equal(t, 0, m.ExecCursor())
	assert.NotEqual(t, before, m.View(), "wheel inside response text should move text, not history")
}

func TestUpdate_Mouse_ResponseTextWheelClampsWithoutHistoryFallback(t *testing.T) {
	t.Parallel()

	m := longResponseMouseModel(t)
	x, y, ok := m.ResponseTextWheelPos()
	require.True(t, ok)

	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
	})
	assert.Equal(t, 0, m.ExecCursor(), "scrolling above the text must not go to history")
	assert.Equal(t, 0, m.ResponseTextOffset(), "text should remain clamped at the top")

	for i := 0; i < 100; i++ {
		m = callUpdate(t, m, tea.MouseMsg{
			X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
		})
	}
	bottom := m.ResponseTextOffset()
	assert.Greater(t, bottom, 0, "setup should reach a non-zero text offset")
	assert.Equal(t, 0, m.ExecCursor(), "scrolling down at the text bottom must not go to history")

	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})
	assert.Equal(t, bottom, m.ResponseTextOffset(), "text should remain clamped at the bottom")
	assert.Equal(t, 0, m.ExecCursor(), "extra bottom scrolling must not change history")
}

func TestUpdate_Mouse_ResponseOutsideTextCyclesHistoryRegardlessOfBodyLength(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "short body", body: "short"},
		{name: "long body", body: longResponseBody()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := responseHistoryMouseModel(t, tc.body)
			x, y, ok := m.ResponsePaneWheelPos()
			require.True(t, ok)

			m = callUpdate(t, m, tea.MouseMsg{
				X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
			})
			assert.Equal(t, 1, m.ExecCursor(), "response-pane chrome should navigate history")

			m = callUpdate(t, m, tea.MouseMsg{
				X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
			})
			assert.Equal(t, 0, m.ExecCursor(), "response-pane chrome should navigate back")
		})
	}
}

func TestUpdate_Mouse_ResponseHistoryPopupWheelIsHistoryNotText(t *testing.T) {
	t.Parallel()

	execs := make([]*domain.Execution, 0, 4)
	for i := 0; i < 4; i++ {
		execs = append(execs, &domain.Execution{
			ID: fmt.Sprintf("ex-%d", i), RequestID: "req-1", StatusCode: 200,
			ResponseBody: longResponseBody(), ResponseHeaders: `{}`,
		})
	}
	m := resizedMouseUnitModel(t).
		WithExecutions(execs).
		WithExecCursor(2).
		WithActiveField(tui.NoneField).
		WithFocus(tui.ResponsePane)
	x, y, ok := m.ResponseHistoryRowClickPos(0)
	require.True(t, ok)

	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})
	assert.Equal(t, 3, m.ExecCursor(), "wheel over the history popup should advance history")
}

func TestUpdate_Mouse_ResponseTextWheelWorksForRawTab(t *testing.T) {
	t.Parallel()

	m := longResponseMouseModel(t).WithResponseTab(tui.RawTab)
	x, y, ok := m.ResponseTextWheelPos()
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})

	assert.Greater(t, m.ResponseTextOffset(), 0, "raw response body should use the scrollable text component")
	assert.Equal(t, 0, m.ExecCursor(), "raw text scrolling must not change history")
}

func TestUpdate_Mouse_DoubleClickResponseTextOpensViewer(t *testing.T) {
	t.Parallel()

	ex := &domain.Execution{
		ID: "ex-viewer", RequestID: "req-1", StatusCode: 200,
		ResponseBody: strings.Repeat("response line\n", 20), ResponseHeaders: `{}`,
	}
	m := resizedMouseUnitModel(t).
		WithExecutions([]*domain.Execution{ex}).
		WithExecCursor(0).
		WithResponseTab(tui.BodyTab)

	x, y, ok := m.ResponseTextWheelPos()
	require.True(t, ok)
	click := tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m = callUpdate(t, m, click)
	m = callUpdate(t, m, click)

	assert.Equal(t, tui.ViewerMode, m.Mode())
	assert.Contains(t, m.View(), "[f] find")
	assert.Contains(t, m.View(), "[c] copy body")
}

func TestUpdate_ViewerOwnsTabAndFinderLifecycle(t *testing.T) {
	t.Parallel()

	m := longResponseMouseModel(t)
	x, y, ok := m.ResponseTextWheelPos()
	require.True(t, ok)
	click := tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m = callUpdate(t, m, click)
	m = callUpdate(t, m, click)
	require.Equal(t, tui.ViewerMode, m.Mode())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	assert.True(t, m.ViewerFindOpen())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.False(t, m.ViewerFindOpen(), "Tab must close the viewer finder instead of changing panes")
	assert.Equal(t, tui.ViewerMode, m.Mode())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.NormalMode, m.Mode(), "Tab must close the viewer after the finder is closed")
}

func TestUpdate_ViewerScrollsAndIgnoresShiftMouse(t *testing.T) {
	t.Parallel()

	m := longResponseMouseModel(t)
	x, y, ok := m.ResponseTextWheelPos()
	require.True(t, ok)
	click := tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m = callUpdate(t, m, click)
	m = callUpdate(t, m, click)
	require.Equal(t, tui.ViewerMode, m.Mode())

	before := m.ViewerTextOffset()
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})
	assert.Greater(t, m.ViewerTextOffset(), before)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Shift: true, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.ViewerMode, m.Mode())
}

func TestUpdate_Keyboard_ResponseTextScrollDoesNotCycleHistory(t *testing.T) {
	t.Parallel()

	m := longResponseMouseModel(t)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	downOffset := m.ResponseTextOffset()
	assert.Greater(t, downOffset, 0, "Down should scroll response text")
	assert.Equal(t, 0, m.ExecCursor(), "Down should not cycle response history")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	pageOffset := m.ResponseTextOffset()
	assert.Greater(t, pageOffset, downOffset, "PageDown should advance response text")
	assert.Equal(t, 0, m.ExecCursor(), "PageDown should not cycle response history")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Less(t, m.ResponseTextOffset(), pageOffset, "Up should scroll response text back")
	assert.Equal(t, 0, m.ExecCursor(), "Up should not cycle response history")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Equal(t, 0, m.ResponseTextOffset(), "PageUp should clamp response text at the top")
	assert.Equal(t, 0, m.ExecCursor(), "PageUp should not cycle response history")
}

func TestUpdate_Keyboard_ShiftResponseNavigationUsesHistory(t *testing.T) {
	t.Parallel()

	m := longResponseMouseModel(t).WithExecCursor(1)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftUp})
	assert.Equal(t, 0, m.ExecCursor(), "Shift+Up should select the previous history item")
	assert.Equal(t, 0, m.ResponseTextOffset(), "Shift+Up should not scroll response text")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftDown})
	assert.Equal(t, 1, m.ExecCursor(), "Shift+Down should select the next history item")
	assert.Equal(t, 0, m.ResponseTextOffset(), "Shift+Down should not scroll response text")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgUp, Alt: true})
	assert.Equal(t, 0, m.ExecCursor(), "modified PageUp should select previous history")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyPgDown, Alt: true})
	assert.Equal(t, 1, m.ExecCursor(), "modified PageDown should select next history")
}

func TestUpdate_Mouse_RequestBodyWheelOnlyScrollsInsideEditor(t *testing.T) {
	t.Parallel()

	req := &domain.Request{
		ID: "req-1", Name: "Post", Method: "POST", URL: "https://example.test",
		Body: longResponseBody(),
	}
	m := requestMouseModel(t).WithActiveRequest(req)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	x, y, ok := m.BodyTextareaWheelPos()
	require.True(t, ok)

	before := m.ViewRequestPaneForTest(90, 18)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})
	after := m.ViewRequestPaneForTest(90, 18)
	assert.NotEqual(t, before, after, "wheel inside request body should scroll its editor")

	outside := callUpdate(t, m, tea.MouseMsg{
		X: x - 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
	})
	assert.Equal(t, after, outside.ViewRequestPaneForTest(90, 18), "wheel outside request body should not move its editor")
}

func TestUpdate_Mouse_RequestBodyPreviewShowsExpectedLinesWhileScrolling(t *testing.T) {
	t.Parallel()

	req := &domain.Request{
		ID: "req-preview", Name: "Large body", Method: "POST", URL: "https://example.test",
		Body: numberedRequestBody(80),
	}
	m := requestMouseModel(t).WithActiveRequest(req).WithFocus(tui.RequestPane)
	before := m.ViewRequestPaneForTest(90, 18)
	assert.Contains(t, before, "BODY_LINE_000")
	assert.NotContains(t, before, "BODY_LINE_079")

	x, y, ok := m.RequestBodyPreviewWheelPos()
	require.True(t, ok)
	for i := 0; i < 100; i++ {
		m = callUpdate(t, m, tea.MouseMsg{
			X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
		})
	}
	middle := m.ViewRequestPaneForTest(90, 18)
	assert.NotContains(t, middle, "BODY_LINE_000", "top lines should scroll out of the preview")
	assert.Contains(t, middle, "BODY_LINE_079", "the preview should reach the end of a large body")
	assert.Greater(t, m.RequestTextOffset(), 0)

	for i := 0; i < 100; i++ {
		m = callUpdate(t, m, tea.MouseMsg{
			X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
		})
	}
	afterUp := m.ViewRequestPaneForTest(90, 18)
	assert.Contains(t, afterUp, "BODY_LINE_000", "scrolling up should reveal the first body line again")
	assert.Equal(t, 0, m.RequestTextOffset())
}

func TestUpdate_Mouse_RequestBodyPreviewIgnoresHorizontalWheel(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t).WithActiveRequest(&domain.Request{
		ID: "req-horizontal", Name: "Large body", Method: "POST", URL: "https://example.test",
		Body: numberedRequestBody(40),
	}).WithFocus(tui.RequestPane)
	x, y, ok := m.RequestBodyPreviewWheelPos()
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelRight,
	})
	assert.Equal(t, 0, m.RequestTextOffset(), "horizontal wheel must not scroll request body")
}

func TestUpdate_Keyboard_ResponseTextScrollsBesideHistoryPopup(t *testing.T) {
	t.Parallel()

	execs := make([]*domain.Execution, 0, 4)
	for i := 0; i < 4; i++ {
		execs = append(execs, &domain.Execution{
			ID: fmt.Sprintf("popup-%d", i), RequestID: "req-popup", StatusCode: 200,
			ResponseBody: longResponseBody(), ResponseHeaders: `{}`,
		})
	}
	m := resizedMouseUnitModel(t).
		WithExecutions(execs).
		WithExecCursor(2).
		WithActiveField(tui.NoneField).
		WithFocus(tui.ResponsePane)
	_ = m.View()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Greater(t, m.ResponseTextOffset(), 0, "plain Down should scroll text even with history popup visible")
	assert.Equal(t, 2, m.ExecCursor(), "plain Down should not navigate history")
}

func numberedRequestBody(count int) string {
	lines := make([]string, count)
	for i := range lines {
		lines[i] = fmt.Sprintf("BODY_LINE_%03d", i)
	}
	return strings.Join(lines, "\n")
}

func longResponseBody() string {
	body := ""
	for i := 0; i < 100; i++ {
		body += fmt.Sprintf("line-%03d\n", i)
	}
	return body
}

func longResponseMouseModel(t *testing.T) tui.Model {
	t.Helper()
	return responseHistoryMouseModel(t, longResponseBody())
}

func responseHistoryMouseModel(t *testing.T, body string) tui.Model {
	t.Helper()
	m := resizedMouseUnitModel(t).
		WithExecutions([]*domain.Execution{
			{ID: "ex-0", RequestID: "req-1", StatusCode: 200, ResponseBody: body, ResponseHeaders: `{}`},
			{ID: "ex-1", RequestID: "req-1", StatusCode: 200, ResponseBody: "older", ResponseHeaders: `{}`},
		}).
		WithExecCursor(0).
		WithActiveField(tui.NoneField).
		WithFocus(tui.ResponsePane)
	_ = m.View()
	return m
}

func TestUpdate_Mouse_ResponseHistoryPopupClick(t *testing.T) {
	t.Parallel()

	execs := make([]*domain.Execution, 0, 4)
	for i := 0; i < 4; i++ {
		execs = append(execs, &domain.Execution{
			ID: fmt.Sprintf("ex-%d", i), RequestID: "req-1", StatusCode: 200,
			ResponseBody: fmt.Sprintf("body-%d", i), ResponseHeaders: `{}`,
		})
	}
	m := resizedMouseUnitModel(t).
		WithExecutions(execs).
		WithExecCursor(2). // historical view shows popup
		WithFocus(tui.ResponsePane)

	x, y, ok := m.ResponseHistoryRowClickPos(0)
	require.True(t, ok, "history popup row should be clickable")
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, 0, m.ExecCursor(), "clicking first visible history row selects index 0")
}

func TestUpdate_Mouse_IgnoresUnsupportedInput(t *testing.T) {
	t.Parallel()

	m := callUpdate(t, newModel(defaultConfig()).WithFocus(tui.SidebarPane),
		tea.WindowSizeMsg{Width: 120, Height: 40})

	tests := []struct {
		name string
		msg  tea.MouseMsg
	}{
		{
			name: "right click",
			msg: tea.MouseMsg{
				X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonRight,
			},
		},
		{
			name: "wheel up",
			msg: tea.MouseMsg{
				X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
			},
		},
		{
			name: "outside layout",
			msg: tea.MouseMsg{
				X: 500, Y: 500, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
			},
		},
		{
			name: "release only",
			msg: tea.MouseMsg{
				X: 5, Y: 5, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := callUpdate(t, m, tt.msg)
			assert.Equal(t, tui.SidebarPane, got.Focus())
		})
	}
}

func TestUpdate_Mouse_IgnoredInOverlayModes(t *testing.T) {
	t.Parallel()

	m := callUpdate(
		t,
		newModel(defaultConfig()).WithMode(tui.SearchMode).WithFocus(tui.RequestPane),
		tea.WindowSizeMsg{Width: 120, Height: 40},
	)

	got := callUpdate(t, m, tea.MouseMsg{
		X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.SearchMode, got.Mode())
	assert.Equal(t, tui.RequestPane, got.Focus())
}

func sidebarMouseModel(t *testing.T) tui.Model {
	t.Helper()
	cols := []*domain.Collection{
		{ID: "col-1", Name: "Alpha"},
		{ID: "col-2", Name: "Beta"},
	}
	m := newModel(defaultConfig()).
		WithCollections(cols).
		WithFocus(tui.SidebarPane)
	return callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
}

func TestUpdate_Mouse_SidebarSelectsCollection(t *testing.T) {
	t.Parallel()

	m := sidebarMouseModel(t)
	x, y, ok := m.SidebarCollectionClickPos(1)
	require.True(t, ok)

	got := callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, 1, got.ColCursor())
	assert.Equal(t, -1, got.ReqCursor())
	assert.Equal(t, tui.SidebarPane, got.Focus())
}

func TestUpdate_Mouse_SidebarWheelMovesCursor(t *testing.T) {
	t.Parallel()

	m := sidebarMouseModel(t)
	got := callUpdate(t, m, tea.MouseMsg{
		X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	})
	assert.Equal(t, 1, got.ColCursor())
}

func TestUpdate_Mouse_SidebarDisclosureExpands(t *testing.T) {
	t.Parallel()

	m := sidebarMouseModel(t)
	x, y, ok := m.SidebarCollectionDisclosurePos(1)
	require.True(t, ok)

	got := callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.True(t, got.IsExpanded("col-2"))
	assert.Equal(t, 1, got.ColCursor())
}

func TestUpdate_Mouse_SelectedCollectionRowTogglesExpand(t *testing.T) {
	t.Parallel()

	m := sidebarMouseModel(t) // colCursor=0, reqCursor=-1
	x, y, ok := m.SidebarCollectionClickPos(0)
	require.True(t, ok)

	click := tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	}
	got := callUpdate(t, m, click)
	assert.True(t, got.IsExpanded("col-1"))

	got = callUpdate(t, got, click)
	assert.False(t, got.IsExpanded("col-1"))
}

func requestMouseModel(t *testing.T) tui.Model {
	t.Helper()
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Get JSON", Method: "GET", URL: "https://example.test/json",
	}
	m := tui.New(tui.Deps{
		Config:   defaultConfig(),
		Executor: &stubExecutor{},
		Ctx:      context.Background(),
	}).
		WithCollections([]*domain.Collection{col}).
		WithActiveRequest(req).
		WithMethod("GET").
		WithURLValue("https://example.test/json").
		WithFocus(tui.RequestPane)
	return callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
}

type stubExecutor struct{}

func (stubExecutor) Execute(
	_ context.Context,
	_ *domain.Request,
) (*exec.ExecuteResult, error) {
	return &exec.ExecuteResult{StatusCode: 200, Status: "200 OK"}, nil
}

func TestUpdate_Mouse_RequestMethodBadgeCycles(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t)
	x, y, ok := m.RequestMethodBadgeClickPos()
	require.True(t, ok)

	want := []string{"POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "GET"}
	for _, method := range want {
		got := callUpdate(t, m, tea.MouseMsg{
			X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		})
		assert.Equal(t, method, got.Method())
		m = got
	}
}

func TestUpdate_Mouse_RequestURLLineBeginsEdit(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)

	got := callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.URLField, got.ActiveField())
	assert.Equal(t, tui.RequestPane, got.Focus())
}

func TestUpdate_Mouse_RequestSendButtonDispatches(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t)
	x, y, ok := m.RequestSendButtonClickPos()
	require.True(t, ok)

	got, cmd := m.Update(tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	gotModel, ok := got.(tui.Model)
	require.True(t, ok)
	assert.True(t, gotModel.Loading())
	require.NotNil(t, cmd)
}

func TestUpdate_Mouse_RequestSendIgnoredWhileLoading(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t).WithLoading(true)
	x, y, ok := m.RequestSendButtonClickPos()
	require.True(t, ok)

	_, cmd := m.Update(tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Nil(t, cmd)
}

func TestUpdate_Mouse_RequestContentClickDoesNotEdit(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t)
	x, y, ok := m.RequestPaneContentClickPos()
	require.True(t, ok)

	got := callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.NoneField, got.ActiveField())
	assert.Equal(t, tui.RequestPane, got.Focus())
}

func TestUpdate_Mouse_URLClickSetsCursor(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t).WithURLValue("https://example.test/path")
	x, y, ok := m.RequestURLTextClickPosAtColumn(8)
	require.True(t, ok)

	got := callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.URLField, got.ActiveField())
	assert.Equal(t, 8, got.URLCursorPosition())

	got = callUpdate(t, got, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	assert.Equal(t, "https://Xexample.test/path", got.URLValue())
}

func TestUpdate_Mouse_URLClickWideCharactersDoNotPanic(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t).WithURLValue("a世b")
	x, y, ok := m.RequestURLTextClickPosAtColumn(2)
	require.True(t, ok)

	assert.NotPanics(t, func() {
		_ = callUpdate(t, m, tea.MouseMsg{
			X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		})
	})
}

func headerListMouseModel(t *testing.T, headers string) tui.Model {
	t.Helper()
	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Get", Method: "GET", URL: "https://example.test",
		Headers: headers,
	}
	m := tui.New(tui.Deps{
		Config: defaultConfig(), Executor: &stubExecutor{}, Ctx: context.Background(),
	}).
		WithCollections([]*domain.Collection{col}).
		WithActiveRequest(req).
		WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	return m
}

func TestUpdate_Mouse_HeaderListClickSelectsAndEditsRow(t *testing.T) {
	t.Parallel()

	m := headerListMouseModel(t, `{"A-Key":"a","B-Key":"b","C-Key":"c"}`)
	require.Equal(t, tui.HeadersField, m.ActiveField())
	require.False(t, m.HeaderEditing())
	require.Len(t, m.HeaderPairs(), 3)

	// Determine which pair renders in row index 1 (map order is deterministic
	// after parse+sort in the editor's slice; assert against that slice).
	targetKey := m.HeaderPairs()[1].Key

	x, y, ok := m.HeaderListRowClickPos(1)
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})

	assert.Equal(t, 1, m.HeaderCursor())
	assert.True(t, m.HeaderEditing(), "clicking a header row must open it for editing")

	// The edited key input should hold the clicked row's key.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // confirm pair edit
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // save
	assert.Contains(t, m.ActiveRequest().Headers, targetKey)
}

func TestUpdate_Mouse_HeaderListClickOutsideRowsIsNoop(t *testing.T) {
	t.Parallel()

	m := headerListMouseModel(t, `{"A-Key":"a"}`)
	require.Len(t, m.HeaderPairs(), 1)

	x, _, ok := m.HeaderListRowClickPos(0)
	require.True(t, ok)

	// Click well below the single row.
	got := callUpdate(t, m, tea.MouseMsg{
		X: x, Y: 30, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.False(t, got.HeaderEditing())
}

func TestUpdate_Mouse_HeaderEditClickFocusesInputs(t *testing.T) {
	t.Parallel()

	m := headerListMouseModel(t, `{"X-Test":"old"}`)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.True(t, m.HeaderEditing())

	x, y, ok := m.HeaderValueInputClickPosAtColumn(20)
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	for _, r := range "new" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.Contains(t, m.ActiveRequest().Headers, `"X-Test"`)
	assert.Contains(t, m.ActiveRequest().Headers, `"oldnew"`)
}

func TestUpdate_Mouse_BodyClickSetsCursor(t *testing.T) {
	t.Parallel()

	col := &domain.Collection{ID: "col-1", Name: "API"}
	req := &domain.Request{
		ID: "req-1", Name: "Post", Method: "POST", URL: "https://example.test",
		Body: "line one\nline two\nline three",
	}
	m := tui.New(tui.Deps{
		Config: defaultConfig(), Executor: &stubExecutor{}, Ctx: context.Background(),
	}).
		WithCollections([]*domain.Collection{col}).
		WithActiveRequest(req).
		WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	x, y, ok := m.BodyTextareaClickPos(1, 3)
	require.True(t, ok)
	got := callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	got = callUpdate(t, got, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	assert.Equal(t, "line one\nlin!e two\nline three", got.BodyValue())
}

func TestUpdate_Mouse_EditorBlankClickKeepsBuffer(t *testing.T) {
	t.Parallel()

	m := requestMouseModel(t).WithURLValue("https://example.test/path")
	x, y, ok := m.RequestURLTextClickPosAtColumn(8)
	require.True(t, ok)
	m = callUpdate(t, m, tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})

	px, py, ok := m.RequestPaneContentClickPos()
	require.True(t, ok)
	got := callUpdate(t, m, tea.MouseMsg{
		X: px, Y: py, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.URLField, got.ActiveField())
	assert.Contains(t, got.URLValue(), "X")
}
