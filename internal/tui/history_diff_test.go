package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// step sends one message to the model, as the program would.
func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	m, _ = stepCmd(t, m, msg)
	return m
}

// stepCmd is step that also returns the command the update produced.
func stepCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	require.True(t, ok, "Update returned %T", updated)
	return next, cmd
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// markTestModel shows two runs of req-1 with the response pane focused.
func markTestModel(t *testing.T) (Model, *historyReader) {
	t.Helper()
	reader := &historyReader{entries: historyTestEntries()}
	m := historyModel(reader)
	m.focus = responsePane
	m.executions = []*domain.Execution{
		{
			ID: "new", RequestID: "req-1", StatusCode: 200,
			ResponseBody: "newest body", CompletedAt: time.Now(),
		},
		{
			ID: "old", RequestID: "req-1", StatusCode: 500,
			ResponseBody: "older body", CompletedAt: time.Now().Add(-time.Minute),
		},
	}
	return m, reader
}

func TestDiffLines_AlignsCommonLines(t *testing.T) {
	out, changed := diffLines([]string{"x", "y", "z"}, []string{"x", "w", "z"})
	assert.True(t, changed)
	assert.Equal(t, []string{"  x", "- y", "+ w", "  z"}, out)
}

func TestDiffLines_IdenticalIsUnchanged(t *testing.T) {
	out, changed := diffLines([]string{"a", "b"}, []string{"a", "b"})
	assert.False(t, changed)
	assert.Equal(t, []string{"  a", "  b"}, out)
}

func TestDiffLines_OneSideEmpty(t *testing.T) {
	out, changed := diffLines(nil, []string{"only"})
	assert.True(t, changed)
	assert.Equal(t, []string{"+ only"}, out)
}

func TestDiffLines_LargeBlocksFallBackToBlocks(t *testing.T) {
	a := make([]string, 2100)
	b := make([]string, 2100)
	for i := range a {
		a[i] = "a" + string(rune('0'+i%10)) + strings.Repeat("x", i%7)
		b[i] = "b" + string(rune('0'+i%10)) + strings.Repeat("y", i%5)
	}
	out, changed := diffLines(a, b)
	require.True(t, changed)
	assert.Contains(t, out[0], "too large to align: 2100 and 2100 lines differ")
	assert.Len(t, out, 1+2100+2100)
}

func TestHistoryDiffText_ComparesStatusHeadersAndBody(t *testing.T) {
	completed := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	a := &domain.Execution{
		ID: "run-a", StatusCode: 200, CompletedAt: completed,
		ResponseHeaders: `{"Content-Type":["application/json"],"X-Old":["1"]}`,
		ResponseBody:    `{"id":1,"name":"one"}`,
	}
	b := &domain.Execution{
		ID: "run-b", StatusCode: 500, CompletedAt: completed.Add(time.Hour),
		ResponseHeaders: `{"Content-Type":["application/json"],"X-New":["2"]}`,
		ResponseBody:    "{\n  \"id\": 1,\n  \"name\": \"two\"\n}",
	}
	out := historyDiffText(a, b)
	assert.Contains(t, out, "  - A  ")
	assert.Contains(t, out, "  + B  ")
	assert.Contains(t, out, "Status (differs)\n- 200\n+ 500\n")
	assert.Contains(t, out, "Headers (differs)\n")
	assert.Contains(t, out, "- X-Old: 1\n")
	assert.Contains(t, out, "+ X-New: 2\n")
	assert.Contains(t, out, "  Content-Type: application/json\n")
	assert.Contains(t, out, "Body (differs)\n")
	assert.Contains(t, out, "-   \"name\": \"one\"\n")
	assert.Contains(t, out, "+   \"name\": \"two\"\n")
}

func TestHistoryDiffText_SameBodyAndStatusAreMarkedSame(t *testing.T) {
	a := &domain.Execution{
		ID: "a", StatusCode: 204, ResponseBody: `{"ok": true}`, ResponseHeaders: "{}",
	}
	b := &domain.Execution{
		ID: "b", StatusCode: 204, ResponseBody: `{"ok":true}`, ResponseHeaders: "{}",
	}
	out := historyDiffText(a, b)
	assert.Contains(t, out, "Status (same)")
	assert.Contains(t, out, "Headers (same)")
	assert.Contains(t, out, "Body (same)")
	assert.NotContains(t, out, "differs")
}

func TestHistoryDiffText_NoBodyAndTruncationNote(t *testing.T) {
	a := &domain.Execution{ID: "a", StatusCode: 0, Error: "dial\n failed", ResponseHeaders: "{"}
	b := &domain.Execution{
		ID: "b", StatusCode: 200, ResponseBody: "part", ResponseBodyTruncated: true,
		ResponseSize: 4096, ResponseHeaders: "{}",
	}
	out := historyDiffText(a, b)
	assert.Contains(t, out, "error: dial failed")
	assert.Contains(t, out, "no response")
	assert.Contains(t, out, "+ part")
	assert.Contains(t, out, "[stored first 4 of 4096 bytes]")

	empty := historyDiffText(&domain.Execution{ID: "x"}, &domain.Execution{ID: "y"})
	assert.Contains(t, empty, "(no stored body in either entry)")
}

func TestMarks_ToggleLimitAndLetters(t *testing.T) {
	m, _ := markTestModel(t)

	m = step(t, m, keyRune('m'))
	assert.Equal(t, []string{"new"}, m.HistoryMarks())
	assert.Contains(t, m.StatusSuccess(), "as A")

	m = step(t, m, keyRune('j'))
	m = step(t, m, keyRune('m'))
	assert.Equal(t, []string{"new", "old"}, m.HistoryMarks())
	assert.Contains(t, m.StatusSuccess(), "press D to compare")

	popup := m.HistoryPopupView(120, 30)
	assert.Contains(t, popup, "A ")
	assert.Contains(t, popup, "B ")

	// Marking a marked entry unmarks it; the letters follow the mark order.
	m = m.WithExecCursor(0)
	m = step(t, m, keyRune('m'))
	assert.Equal(t, []string{"old"}, m.HistoryMarks())
	m = step(t, m, keyRune('m'))
	assert.Equal(t, []string{"old", "new"}, m.HistoryMarks())

	// A third entry cannot be marked while two are marked.
	m = m.WithExecutions(append(m.Executions(), &domain.Execution{
		ID: "third", RequestID: "req-1", CompletedAt: time.Now().Add(-2 * time.Minute),
	}))
	m = m.WithExecCursor(2)
	m = step(t, m, keyRune('m'))
	assert.Equal(t, []string{"old", "new"}, m.HistoryMarks())
	assert.Contains(t, m.StatusErr(), "unmark one first")
}

func TestMarks_PopupShowsOnLatestWhenMarked(t *testing.T) {
	m, _ := markTestModel(t)
	assert.Empty(t, m.HistoryPopupView(120, 30), "latest entry without marks hides the popup")

	m = step(t, m, keyRune('m'))
	assert.Contains(t, m.HistoryPopupView(120, 30), "A ")
}

func TestMarks_PrunedWhenHistoryReloads(t *testing.T) {
	m, _ := markTestModel(t)
	m = step(t, m, keyRune('m'))
	m = step(t, m, keyRune('j'))
	m = step(t, m, keyRune('m'))

	m = step(t, m, executionHistoryLoadedMsg{
		requestID:  "req-1",
		executions: []*domain.Execution{{ID: "old", RequestID: "req-1"}},
	})
	assert.Equal(t, []string{"old"}, m.HistoryMarks())
}

func TestCompare_NeedsTwoMarks(t *testing.T) {
	m, _ := markTestModel(t)
	m = step(t, m, keyRune('m'))
	m, cmd := stepCmd(t, m, keyRune('D'))
	assert.Nil(t, cmd)
	assert.Contains(t, m.StatusErr(), "Mark two history entries")
}

func TestCompare_OpensDiffInViewer(t *testing.T) {
	m, reader := markTestModel(t)
	m = step(t, m, keyRune('m'))
	m = step(t, m, keyRune('j'))
	m = step(t, m, keyRune('m'))

	m, cmd := stepCmd(t, m, keyRune('D'))
	require.NotNil(t, cmd)
	msg := cmd()
	loaded, ok := msg.(historyDiffLoadedMsg)
	require.True(t, ok, "compare loads both entries: %T", msg)
	assert.Equal(t, []string{"new", "old"}, reader.gets)
	assert.Equal(t, "new", loaded.first.ID, "the first mark is A")
	assert.Equal(t, "old", loaded.second.ID, "the second mark is B")

	m = step(t, m, loaded)
	assert.Equal(t, viewerMode, m.Mode())
	assert.Contains(t, m.ViewerContent(), "Status (differs)")
	assert.Contains(t, m.ViewerContent(), "- 200", "A is the first mark, status 200")
	assert.Contains(t, m.ViewerContent(), "+ 500", "B is the second mark, status 500")
}

func TestCompare_IgnoresDiffFromAnotherRequest(t *testing.T) {
	m, _ := markTestModel(t)
	m = step(t, m, historyDiffLoadedMsg{
		requestID: "other",
		first:     &domain.Execution{ID: "a"},
		second:    &domain.Execution{ID: "b"},
	})
	assert.Equal(t, normalMode, m.Mode())
}

func TestCompare_ReportsLoadFailure(t *testing.T) {
	m, reader := markTestModel(t)
	reader.getErr = errors.New("boom")
	m = step(t, m, keyRune('m'))
	m = step(t, m, keyRune('j'))
	m = step(t, m, keyRune('m'))
	m, cmd := stepCmd(t, m, keyRune('D'))
	require.NotNil(t, cmd)
	m = step(t, m, cmd())
	assert.Contains(t, m.err.Error(), "boom")
}
