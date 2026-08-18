//go:build e2e

package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

const viewerE2EWidth = 100
const viewerE2EHeight = 30

func viewerResponseBody() []byte {
	lines := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		lines = append(lines, "viewer-line-"+string(rune('a'+i%26))+" needle")
	}
	return []byte(strings.Join(lines, "\n"))
}

func newViewerE2EModel(t *testing.T) tui.Model {
	t.Helper()
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, viewerE2EWidth, viewerE2EHeight)
	m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       viewerResponseBody(),
	}))
	return m
}

func openViewerE2E(t *testing.T, m tui.Model) tui.Model {
	t.Helper()
	x, y, ok := m.ResponseTextWheelPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	m = callUpdate(t, m, click(x, y))
	return m
}

func TestE2E_Viewer_DoubleClickIsFullscreenAndKeepsHelpAtBottom(t *testing.T) {
	m := openViewerE2E(t, newViewerE2EModel(t))

	assert.Equal(t, tui.ViewerMode, m.Mode())
	assertViewContains(t, m, "[f] find")
	assertViewContains(t, m, "[c] copy body")
	assertViewContains(t, m, "[shift+click] terminal select")
	assertViewNotContains(t, m, "Collections")
	assertViewNotContains(t, m, "Request")
	assertViewNotContains(t, m, "Response")
	assert.Equal(t, viewerE2EHeight, lipgloss.Height(m.View()))
	assert.LessOrEqual(t, lipgloss.Width(m.View()), viewerE2EWidth)
}

func TestE2E_Viewer_FinderOwnsTabAndReturnsToViewer(t *testing.T) {
	m := openViewerE2E(t, newViewerE2EModel(t))
	m = callUpdate(t, m, tuitestKey('f'))
	require.True(t, m.ViewerFindOpen())
	assertViewContains(t, m, "Find:")

	m = callUpdate(t, m, tuitestRunes("needle"))
	assert.Equal(t, 120, m.ViewerMatchCount())
	assert.Contains(t, m.View(), "needle", "the active match text should remain visible")
	assert.Contains(t, m.View(), "\x1b[48;5;214m", "find matches should have a visible highlight style")
	assert.Contains(t, m.View(), "match 1/120")
	first := m.ViewerTextOffset()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.ViewerMode, m.Mode(), "Enter must iterate, not close the viewer")
	assert.Greater(t, m.ViewerTextOffset(), first)
	assert.Contains(t, m.View(), "match 2/120")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	assert.Equal(t, first, m.ViewerTextOffset(), "Shift+Enter should move to the previous match")
	assert.Contains(t, m.View(), "match 1/120")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tui.ViewerMode, m.Mode(), "Tab must close only the finder")
	assert.False(t, m.ViewerFindOpen())
	assertViewContains(t, m, "[f] find")
}

func TestE2E_Viewer_ControlFOpensFinder(t *testing.T) {
	m := openViewerE2E(t, newViewerE2EModel(t))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	assert.True(t, m.ViewerFindOpen())
	assertViewContains(t, m, "Find:")
}

func TestE2E_Viewer_EscAndTabCloseModal(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyTab} {
		t.Run(key.String(), func(t *testing.T) {
			m := openViewerE2E(t, newViewerE2EModel(t))
			m = callUpdate(t, m, tea.KeyMsg{Type: key})
			assert.Equal(t, tui.NormalMode, m.Mode())
			assertViewNotContains(t, m, "[f] find")
		})
	}
}

func TestE2E_Viewer_ScrollsAndShiftClickDoesNotCloseOrScroll(t *testing.T) {
	m := openViewerE2E(t, newViewerE2EModel(t))
	before := m.ViewerTextOffset()
	m = callUpdate(t, m, wheelDown(10, 10))
	assert.Greater(t, m.ViewerTextOffset(), before)

	afterWheel := m.ViewerTextOffset()
	m = callUpdate(t, m, tea.MouseMsg{
		X: 10, Y: 10, Shift: true,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	assert.Equal(t, tui.ViewerMode, m.Mode())
	assert.Equal(t, afterWheel, m.ViewerTextOffset())
}

func TestE2E_Viewer_RequestPreviewAlsoOpens(t *testing.T) {
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{}).
		WithFocus(tui.RequestPane).
		WithActiveRequest(&domain.Request{Body: "request-body\nsecond-line"})
	m = resize(t, m, viewerE2EWidth, viewerE2EHeight)
	x, y, ok := m.RequestBodyPreviewWheelPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	m = callUpdate(t, m, click(x, y))
	assert.Equal(t, tui.ViewerMode, m.Mode())
	assertViewContains(t, m, "request-body")
}

func TestE2E_Viewer_CountsMultipleMatchesOnOneLine(t *testing.T) {
	m := newViewerE2EModel(t)
	m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       []byte("prefix dy middle dy suffix"),
	}))
	m = openViewerE2E(t, m)
	m = callUpdate(t, m, tuitestKey('f'))
	m = callUpdate(t, m, tuitestRunes("dy"))

	assert.Equal(t, 2, m.ViewerMatchCount())
	assert.Contains(t, m.View(), "match 1/2")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Contains(t, m.View(), "match 2/2")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	assert.Contains(t, m.View(), "match 1/2")
}

func tuitestKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func tuitestRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
