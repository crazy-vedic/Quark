package tui_test

// Bug-fix regression tests — one test per bug ID from qa-ux-bug-report.md.

import (
	"errors"
	"fmt"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

// --- BUG-001: editingURL not cleared on import modal Escape ---

func TestBug001_EscapeFromImportMode_ClearsEditingURL(t *testing.T) {
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

func TestBug001_EscapeFromImportMode_GlobalKeysWork(t *testing.T) {
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

func TestBug003_InvalidURLError_NoDuplicatePrefix(t *testing.T) {
	m := newTestModel()
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

func TestBug004_StubKey_a_InSidebar_ShowsFeedback(t *testing.T) {
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

func TestBug004_StubKey_b_InRequest_NoActiveRequest(t *testing.T) {
	m := newTestModel()
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.NotEmpty(
		t,
		m.StatusErr(),
		"pressing 'b' with no active request must show a status message",
	)
}

func TestBug004_StubKey_h_InRequest_NoActiveRequest(t *testing.T) {
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

func TestBug008_SearchModal_BeforeSearch_NotSearchedYet(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, tui.SearchMode, m.Mode())
	assert.False(
		t,
		m.Searched(),
		"Searched must be false when modal first opens, before any result arrives",
	)
}

func TestBug008_SearchModal_AfterResultsMsg_MarkedSearched(t *testing.T) {
	m := newTestModel()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = callUpdate(t, m, tui.SearchResultsMsg(nil))
	assert.True(t, m.Searched(), "Searched must be true after first SearchResultsMsg received")
}

func TestBug008_SearchModal_ReopenClearsStaleQuery(t *testing.T) {
	m := newTestModel().WithSearchInputValue("post")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, tui.SearchMode, m.Mode())
	assert.Equal(t, "", m.SearchInputValue(), "search input must be cleared when search opens")
}

// --- BUG-009: pressing 'q' in help mode closes help instead of quitting ---

func TestBug009_QInHelpMode_Quits(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.HelpMode)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	require.NotNil(t, cmd, "pressing 'q' in help mode must return a non-nil command")
	msg := cmd() // invoke the Cmd to get the message
	assert.IsType(t, tea.QuitMsg{}, msg, "pressing 'q' in help mode must return tea.Quit")
}

func TestBug009_NavKeyInHelpMode_MovesCursor(t *testing.T) {
	m := newTestModel()
	m = m.WithMode(tui.HelpMode)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, tui.HelpMode, m.Mode(), "navigation key in help must stay in help mode")
	assert.Equal(t, 1, m.HelpCursor(), "j in help must move cursor down")
}

// --- BUG-007: old streamed response temp file not cleaned up on new response ---

func TestBug007_NewResponse_CleansUpOldTempFile(t *testing.T) {
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

func TestBug007_FirstResponse_NoCleanupNeeded(t *testing.T) {
	// No prior response — should not panic.
	m := newTestModel()
	newResult := &exec.ExecuteResult{StatusCode: 200, Body: []byte("hello")}
	m = callUpdate(t, m, tui.HttpResponseMsg(newResult))
	assert.NotNil(t, m.Response(), "response must be stored")
}

// --- BUG-008: searchCancel not called when Esc dismisses search mode ---

func TestBug008_EscapeFromSearchMode_CallsSearchCancel(t *testing.T) {
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

func TestBug008_EscapeFromSearchMode_NilCancelOK(t *testing.T) {
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
