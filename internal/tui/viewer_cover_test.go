package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestViewerAbs(t *testing.T) {
	assert.Equal(t, 3, viewerAbs(-3))
	assert.Equal(t, 4, viewerAbs(4))
	assert.Equal(t, 0, viewerAbs(0))
}

func TestCountFoldMatches(t *testing.T) {
	assert.Equal(t, 3, countFoldMatches("Hello hello HELLO", "hello"))
	assert.Equal(t, 0, countFoldMatches("abc", ""))
	assert.Equal(t, 0, countFoldMatches("ab", "abc"))
	assert.Equal(t, 2, countFoldMatches("aaaa", "aa"), "matches are non-overlapping")
}

func TestViewerPreviousMatchKey(t *testing.T) {
	assert.True(t, viewerPreviousMatchKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true}))
	assert.False(t, viewerPreviousMatchKey(tea.KeyMsg{Type: tea.KeyEnter}))
}

func TestHighlightViewerMatches(t *testing.T) {
	t.Run("empty query returns content unchanged", func(t *testing.T) {
		assert.Equal(t, "say hi", highlightViewerMatches("say hi", "   "))
	})
	t.Run("no match returns content unchanged", func(t *testing.T) {
		assert.Equal(t, "say hi", highlightViewerMatches("say hi", "zzz"))
	})
	t.Run("match is wrapped and ansi preserved", func(t *testing.T) {
		in := "\x1b[31mhello\x1b[0m world"
		out := highlightViewerMatches(in, "ell")
		assert.Contains(t, out, "\x1b[31m")
		assert.Contains(t, out, "\x1b[7m\x1b[1m")
		assert.Contains(t, out, "\x1b[27m\x1b[22m")
		assert.Equal(t, "hello world", stripANSI(out))
	})
	t.Run("match at end is closed", func(t *testing.T) {
		out := highlightViewerMatches("say hi", "hi")
		assert.True(t, strings.HasSuffix(out, "\x1b[27m\x1b[22m"))
		assert.Equal(t, "say hi", stripANSI(out))
	})
}

func TestMoveViewerMatchWraps(t *testing.T) {
	m := &Model{viewerMatches: []int{4, 9, 12}, viewerLastMatch: 0}
	m.moveViewerMatch(-1)
	assert.Equal(t, 2, m.viewerLastMatch)
	assert.Equal(t, 12, m.viewerText.offset)
	m.moveViewerMatch(1)
	assert.Equal(t, 0, m.viewerLastMatch)
	assert.Equal(t, 4, m.viewerText.offset)

	empty := &Model{}
	empty.moveViewerMatch(1)
	assert.Equal(t, 0, empty.viewerLastMatch)
}

func TestViewerMatchStatus(t *testing.T) {
	m := New(Deps{})
	assert.Empty(t, m.viewerMatchStatus())
	m.viewerFind.SetValue("x")
	assert.Equal(t, "  match 0/0", m.viewerMatchStatus())
	m.viewerMatches = []int{1, 2}
	m.viewerLastMatch = 1
	assert.Equal(t, "  match 2/2", m.viewerMatchStatus())
}

func TestViewerClipboardCmdsReturnMessages(t *testing.T) {
	_, ok := copyViewerContentCmd("body")().(viewerClipboardMsg)
	assert.True(t, ok)
	_, ok = copyCurlCommandCmd("curl x")().(curlExportClipboardMsg)
	assert.True(t, ok)
	_, ok = readCurlClipboardCmd()().(curlClipboardMsg)
	assert.True(t, ok)
}

func viewerTestModel(t *testing.T) Model {
	t.Helper()
	m := New(Deps{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = requireModel(t, updated)
	m = m.WithResponse(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       []byte("hello world\nsecond hello line\nthird\nfourth"),
	})
	m = m.openViewerForResponse()
	require.Equal(t, viewerMode, m.mode)
	return m
}

func sendKey(t *testing.T, m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return requireModel(t, updated), cmd
}

func TestViewerFindFlow(t *testing.T) {
	m := viewerTestModel(t)

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	require.True(t, m.ViewerFindOpen())

	for _, r := range "hello" {
		m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	assert.Equal(t, 2, m.ViewerMatchCount())
	assert.Contains(t, m.viewViewer(), "match 1/2")

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Contains(t, m.viewerMatchStatus(), "match 2/2")

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	assert.Contains(t, m.viewerMatchStatus(), "match 1/2")

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.False(t, m.ViewerFindOpen())
	assert.Equal(t, viewerMode, m.mode)

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, normalMode, m.mode)
}

func TestViewerFindSelectionCopyAndCut(t *testing.T) {
	m := viewerTestModel(t)
	m, _ = m.openViewerFind()
	m.viewerFind.SetValue("hello")
	m.viewerFind.SetCursor(5)

	// Ctrl+C without a selection does nothing.
	_, cmd := sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Nil(t, cmd)

	// Select all, then Ctrl+C copies.
	m.viewerFindSelection = textSelection{anchor: 0, cursor: 5}
	_, cmd = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd)

	// Ctrl+X cuts and copies the selection.
	m.viewerFindSelection = textSelection{anchor: 0, cursor: 5}
	cut, cmd := sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd)
	assert.Equal(t, "", cut.viewerFind.Value())

	// Ctrl+X with nothing selected removes nothing and returns no command.
	m.viewerFind.SetValue("abc")
	m.viewerFindSelection = textSelection{}
	cut, cmd = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.Nil(t, cmd)
	assert.Equal(t, "abc", cut.viewerFind.Value())
}

func TestViewerFindSelectionKeysMoveSelection(t *testing.T) {
	m := viewerTestModel(t)
	m, _ = m.openViewerFind()
	m.viewerFind.SetValue("hello")
	m.viewerFind.SetCursor(5)

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	assert.True(t, m.viewerFindSelection.Active())

	// Typing a character in the find field replaces the selection.
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	assert.Equal(t, "z", m.viewerFind.Value())
}

func TestViewerScrollKeys(t *testing.T) {
	m := New(Deps{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = requireModel(t, updated)
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "line")
	}
	m = m.WithResponse(&exec.ExecuteResult{Body: []byte(strings.Join(lines, "\n"))})
	m = m.openViewerForResponse()
	m.viewerText.offset = 2
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, m.ViewerTextOffset())
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, m.ViewerTextOffset())
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	end := m.ViewerTextOffset()
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	assert.LessOrEqual(t, m.ViewerTextOffset(), end)
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, m.ViewerTextOffset())
}

func TestViewerNormalModeKeys(t *testing.T) {
	m := viewerTestModel(t)

	_, cmd := sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assert.NotNil(t, cmd, "c copies the body")

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlF})
	assert.True(t, m.ViewerFindOpen())
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m.ViewerFindOpen())

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Equal(t, viewerMode, m.mode, "unknown keys are ignored")

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, normalMode, m.mode)
}

func TestViewerRequestContent(t *testing.T) {
	m := New(Deps{})
	copied := m.viewerRequestContent()
	assert.Empty(t, copied)

	req := &domain.Request{
		ID:     "r1",
		Method: "POST",
		URL:    "https://example.com",
		Body:   "\x1b[31m{\"a\":1}\x1b[0m",
	}
	m = m.WithActiveRequest(req)
	copied = m.viewerRequestContent()
	assert.Equal(t, `{"a":1}`, copied)

	req.Body = ""
	copied = m.viewerRequestContent()
	assert.Empty(t, copied, "no body and no loaded preview copies nothing")

	m = m.openViewerForRequest()
	assert.Equal(t, viewerMode, m.mode)
}

func TestViewerResponseContent(t *testing.T) {
	m := New(Deps{})
	display, copied := m.viewerResponseContent()
	assert.Empty(t, display)
	assert.Empty(t, copied)

	m = m.WithResponse(&exec.ExecuteResult{Body: []byte("inline body")})
	m.responseTab = bodyTab
	_, copied = m.viewerResponseContent()
	assert.Equal(t, "inline body", copied)

	m.responseTab = headersTab
	_, copied = m.viewerResponseContent()
	assert.NotContains(t, copied, "inline body")

	dir := t.TempDir()
	path := filepath.Join(dir, "resp.txt")
	require.NoError(t, os.WriteFile(path, []byte("from file"), 0o600))
	m = m.WithResponse(&exec.ExecuteResult{TempPath: path})
	m.responseTab = rawTab
	_, copied = m.viewerResponseContent()
	assert.Equal(t, "from file", copied)

	m = m.WithResponse(&exec.ExecuteResult{TempPath: filepath.Join(dir, "missing")})
	_, copied = m.viewerResponseContent()
	assert.Equal(t, "", copied)
}
