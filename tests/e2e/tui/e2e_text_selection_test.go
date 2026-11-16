//go:build e2e

package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

const selectionE2EURL = "https://api.example.com/v1/users"

func newSelectionE2EModel(t *testing.T) tui.Model {
	t.Helper()
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 120, 40)
	m, _ = m.SelectRequest(&domain.Request{
		ID:     "sel-req",
		Name:   "Selection",
		Method: "GET",
		URL:    selectionE2EURL,
	})
	return m
}

// focusURLForEditing clicks the URL line, which begins inline URL editing.
func focusURLForEditing(t *testing.T, m tui.Model) tui.Model {
	t.Helper()
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	require.Equal(t, tui.URLField, m.ActiveField())
	return m
}

func TestE2E_TextSelection_URLKeyboardSelectAndReplace(t *testing.T) {
	m := focusURLForEditing(t, newSelectionE2EModel(t))

	// Move to the end, then extend the selection back to the start.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftHome})
	sel := m.URLSelection()
	assert.True(t, sel.Active())
	start, end := sel.Range()
	assert.Equal(t, 0, start)
	assert.Equal(t, len(selectionE2EURL), end)

	// Shift+Right shrinks the selection from the start (the cursor sits at 0).
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	start, _ = m.URLSelection().Range()
	assert.Equal(t, 1, start)

	// Ctrl+A selects everything again.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	start, end = m.URLSelection().Range()
	assert.Equal(t, 0, start)
	assert.Equal(t, len(selectionE2EURL), end)

	// Typing replaces the whole selection.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Z'}})
	assert.Equal(t, "Z", m.URLValue())
	assert.False(t, m.URLSelection().Active())
}

func TestE2E_TextSelection_URLCopyCutAndPaste(t *testing.T) {
	m := focusURLForEditing(t, newSelectionE2EModel(t))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})

	// Ctrl+C copies without modifying the value.
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd, "copy issues a clipboard command")
	assert.Equal(t, selectionE2EURL, m2.URLValue())

	// Ctrl+X removes the selection and copies it.
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd)
	assert.Equal(t, "", m.URLValue())

	// Pasting multi-rune content replaces nothing when no selection is active.
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(selectionE2EURL), Paste: true},
	)
	assert.Equal(t, selectionE2EURL, m.URLValue())

	// Ctrl+Shift+Left selects by word; backspace removes that selection.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftLeft})
	require.True(t, m.URLSelection().Active())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "https://api.example.com/v1/", m.URLValue())

	// Ctrl+X with nothing selected leaves the value alone.
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.Nil(t, cmd)
	assert.Equal(t, "https://api.example.com/v1/", m.URLValue())
}

func TestE2E_TextSelection_URLDoubleClickSelectsWord(t *testing.T) {
	m := newSelectionE2EModel(t)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)

	// The first click begins editing; a second click in the same cell within the
	// double-click window selects the word under the cursor.
	m = callUpdate(t, m, click(x, y))
	m = callUpdate(t, m, click(x, y))
	assert.True(t, m.URLSelection().Active(), "double-click selects the word under the cursor")
}

func TestE2E_TextSelection_ViewerFindSelectionAndCopy(t *testing.T) {
	m := openViewerE2E(t, newViewerE2EModel(t))
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	require.True(t, m.ViewerFindOpen())

	for _, r := range "needle" {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	assert.Greater(t, m.ViewerMatchCount(), 0)

	// Shift+Left extends a selection inside the find box; Ctrl+C copies it.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	assert.True(t, m.ViewerFindSelection().Active())
	_, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd)

	// Ctrl+A selects the whole query, Ctrl+X cuts it out.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd)
	assert.True(t, m.ViewerFindOpen())
	assert.Equal(t, 0, m.ViewerMatchCount(), "an empty query has no matches")
}

func newSelectionRequestE2EModel(t *testing.T, req *domain.Request) tui.Model {
	t.Helper()
	st := setupStore(t)
	m := newE2EModel(t, st, &mockExecutor{})
	m = resize(t, m, 120, 40)
	m, _ = m.SelectRequest(req)
	return m.WithFocus(tui.RequestPane)
}

func TestE2E_TextSelection_BodyEditorKeyboardFlow(t *testing.T) {
	m := newSelectionRequestE2EModel(t, &domain.Request{
		ID:     "body-sel",
		Name:   "Body selection",
		Method: "POST",
		URL:    selectionE2EURL,
		Body:   "alpha beta gamma",
	})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	require.Equal(t, tui.BodyField, m.ActiveField())

	// Ctrl+A selects the whole body; Ctrl+C copies it without changing it.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.True(t, m.BodySelection().Active())
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd)
	assert.Equal(t, "alpha beta gamma", m2.BodyValue())

	// Ctrl+X cuts the selection; the value empties.
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd)
	assert.Equal(t, "", m.BodyValue())

	// Paste reinserts text, then Ctrl+Shift+Left selects a word and Delete removes it.
	m = callUpdate(
		t,
		m,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("one two three"), Paste: true},
	)
	assert.Equal(t, "one two three", m.BodyValue())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftLeft})
	require.True(t, m.BodySelection().Active())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDelete})
	assert.Equal(t, "one two ", m.BodyValue())

	// Shift+Home selects to the start of the line; typing replaces it.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftHome})
	require.True(t, m.BodySelection().Active())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	assert.Equal(t, "Q", m.BodyValue())

	// Arrow keys clear the selection without modifying the text.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.False(t, m.BodySelection().Active())
	assert.Equal(t, "Qab", m.BodyValue())
}

func TestE2E_TextSelection_HeaderPairKeyboardFlow(t *testing.T) {
	m := newSelectionRequestE2EModel(t, &domain.Request{
		ID:      "header-sel",
		Name:    "Header selection",
		Method:  "GET",
		URL:     selectionE2EURL,
		Headers: `{"Accept":"*/*"}`,
	})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	require.Equal(t, tui.HeadersField, m.ActiveField())

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	require.True(t, m.HeaderEditing())

	// Ctrl+A selects the whole header key; typing replaces it.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.True(t, m.HeaderKeySelection().Active())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Qq-Id")})
	require.True(t, len(m.HeaderPairs()) > 0)
	assert.False(t, m.HeaderKeySelection().Active())

	// Move to the value field and select it; Ctrl+X cuts it.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.True(t, m.HeaderValueSelection().Active())
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.NotNil(t, cmd)
	assert.False(t, m.HeaderValueSelection().Active())
}

func TestE2E_TextSelection_WordMovementAcrossPunctuation(t *testing.T) {
	m := newSelectionRequestE2EModel(t, &domain.Request{
		ID:     "word-sel",
		Name:   "Word movement",
		Method: "POST",
		URL:    selectionE2EURL,
		Body:   "alpha-beta gamma_delta 123",
	})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	require.Equal(t, tui.BodyField, m.ActiveField())

	// Walk backward word by word across hyphens, underscores and digits.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	for i := 0; i < 4; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftLeft})
	}
	require.True(t, m.BodySelection().Active())
	start, end := m.BodySelection().Range()
	assert.Less(t, start, end)
	assert.Equal(t, len("alpha-beta gamma_delta 123"), end)

	// Walking forward from the start selects the next word boundaries.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyHome})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	require.True(t, m.BodySelection().Active())
	_, end = m.BodySelection().Range()
	assert.Greater(t, end, 0)
}

func TestE2E_BodySaveFormatsJSON(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Format.JSONOnSave = true
	st := setupStore(t)
	m := newE2EModelWithConfig(t, st, &mockExecutor{}, cfg)
	m = resize(t, m, 120, 40)
	m, _ = m.SelectRequest(&domain.Request{
		ID:      "json-save",
		Name:    "JSON save",
		Method:  "POST",
		URL:     selectionE2EURL,
		Headers: `{"Content-Type":"application/json"}`,
		Body:    `{"name":"quark","items":[1,2]}`,
	})
	m = m.WithFocus(tui.RequestPane)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	require.Equal(t, tui.BodyField, m.ActiveField())

	// Enter saves the body; valid JSON is pretty-printed on save.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Contains(t, m.BodyValue(), "\n  \"name\": \"quark\"")
	assert.Equal(t, tui.NoneField, m.ActiveField())
}

func TestE2E_TextSelection_MouseHeaderEditing(t *testing.T) {
	m := newSelectionRequestE2EModel(t, &domain.Request{
		ID:      "header-mouse",
		Name:    "Header mouse",
		Method:  "GET",
		URL:     selectionE2EURL,
		Headers: `{"Accept":"*/*"}`,
	})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	require.Equal(t, tui.HeadersField, m.ActiveField())

	// Clicking a header row opens it for inline editing.
	x, y, ok := m.HeaderListRowClickPos(0)
	require.True(t, ok)
	m = callUpdate(t, m, click(x, y))
	require.True(t, m.HeaderEditing())

	// Clicking the key and value inputs moves focus between them.
	kx, ky, ok := m.HeaderKeyInputClickPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(kx, ky))
	vx, vy, ok := m.HeaderValueInputClickPos()
	require.True(t, ok)
	m = callUpdate(t, m, click(vx, vy))
	assert.True(t, m.HeaderEditing())
}
