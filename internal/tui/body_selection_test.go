package tui_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// bodyModel returns a model with the body field in edit mode and the given text.
func bodyModel(t *testing.T, body string) tui.Model {
	req := &domain.Request{Name: "req", Method: "POST", URL: "https://example.com", Body: body}
	m := newModel(defaultConfig()).
		WithFocus(tui.RequestPane).
		WithActiveRequest(req).
		WithBodyEdit() // focuses the textarea, as pressing b does
	// Size the window so the body is laid out and rendered, then start at the
	// top of the text: loading the value leaves the cursor at the end.
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	return callUpdate(t, m, tea.KeyMsg{Type: tea.KeyHome})
}

func TestBodyShiftRightSelectsAndTypingReplaces(t *testing.T) {
	m := bodyModel(t, "hello world")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.True(t, m.BodySelection().Active())
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	assert.Equal(t, "X", m.BodyValue())
	assert.False(t, m.BodySelection().Active())
}

func TestBodyCutAndCopy(t *testing.T) {
	m := bodyModel(t, "abc def")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	assert.Equal(t, "c def", m.BodyValue())
	assert.False(t, m.BodySelection().Active())
}

func TestBodyCopyKeepsTextAndQuitsWithoutSelection(t *testing.T) {
	m := bodyModel(t, "abc")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, "abc", m.BodyValue())

	// Ctrl+C with no selection does nothing; it never quits the app.
	m = bodyModel(t, "abc")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, "abc", m.BodyValue())
}

func TestBodyShiftDownExtendsAcrossWrappedRows(t *testing.T) {
	// Long first line wraps; Shift+Down from the start must select into line 2.
	body := strings.Repeat("a", 120) + "\nsecond"
	m := bodyModel(t, body)
	// The cursor starts at the end of the text. Move up to the first line,
	// then to its start.
	for i := 0; i < 40; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyHome})
	for i := 0; i < 40; i++ {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftDown})
	}
	sel := m.BodySelection()
	assert.True(t, sel.Active())
	start, end := sel.Range()
	assert.Equal(t, 0, start)
	assert.Greater(t, end, 120, "Shift+Down must reach the second logical line")
}

func TestBodyViewShowsSelection(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := bodyModel(t, "select this text")
	plain := m.View()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	selected := m.View()
	assert.NotEqual(t, plain, selected, "a body selection must change the rendered view")
}

// The cursor blinks by sending non-key messages to the body. A blink must not
// clear a selection the user is holding.
func TestBodySelectionSurvivesCursorBlink(t *testing.T) {
	m := bodyModel(t, "select me")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyShiftRight})
	require.True(t, m.BodySelection().Active())
	m = callUpdate(t, m, cursor.BlinkMsg{})
	assert.True(t, m.BodySelection().Active(), "blink must not clear the selection")
}

// Enter saves the body and leaves edit mode; Ctrl+J adds a newline.
func TestBodyEnterSavesAndExits(t *testing.T) {
	m := bodyModel(t, "abc")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, tui.NoneField, m.ActiveField(), "Enter must leave the body editor")
	assert.Equal(t, "abcd", m.ActiveRequest().Body)
}

func TestBodyCtrlJInsertsNewline(t *testing.T) {
	m := bodyModel(t, "ab")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	assert.Equal(t, tui.BodyField, m.ActiveField(), "Ctrl+J must stay in the editor")
	assert.Equal(t, "ab\n", m.BodyValue())
}
