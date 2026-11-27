package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// typeMethodKeys types text into the open method input one key at a time.
func typeMethodKeys(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		updated, _ := m.handleRequestKey("", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = requireModel(t, updated)
	}
	return m
}

func pressMethodEnter(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.handleRequestKey("", tea.KeyMsg{Type: tea.KeyEnter})
	return requireModel(t, updated), cmd
}

func TestEditMethodKeyOpensTheMethodInput(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.method = "PUT"
	m.focus = requestPane

	updated, _ := m.handleRequestAction(keybindings.ActionEditMethod)
	m = requireModel(t, updated)

	require.Equal(t, methodField, m.activeField)
	require.Equal(t, "PUT", m.methodInput.Value())
	require.True(t, m.methodInput.Focused())
	require.Contains(t, m.renderMethodInput(), "PUT")
}

func TestEditMethodCommitsUpperCasedCustomMethodAndSaves(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.activeRequest = &domain.Request{ID: "r1", Method: "GET", URL: "https://example.test"}
	m.method = "GET"
	updated, _ := m.handleRequestAction(keybindings.ActionEditMethod)
	m = requireModel(t, updated)

	m.methodInput.SetValue("")
	m = typeMethodKeys(t, m, " propfind ")
	m, cmd := pressMethodEnter(t, m)

	require.Equal(t, "PROPFIND", m.method)
	require.Equal(t, "PROPFIND", m.activeRequest.Method)
	require.Equal(t, noneField, m.activeField)
	require.False(t, m.methodInput.Focused())
	require.NotNil(t, cmd, "a saved request must be persisted")
}

func TestEditMethodRefusesInvalidTokensAndStaysOpen(t *testing.T) {
	for _, bad := range []string{"", "   ", "GE T", "GET/1", "MÉTHOD"} {
		t.Run(bad, func(t *testing.T) {
			m := New(Deps{Config: config.Default(t.TempDir())})
			m.activeRequest = &domain.Request{ID: "r1", Method: "GET", URL: "https://example.test"}
			m.method = "GET"
			updated, _ := m.handleRequestAction(keybindings.ActionEditMethod)
			m = requireModel(t, updated)

			m.methodInput.SetValue(bad)
			m, cmd := pressMethodEnter(t, m)

			require.Equal(t, methodField, m.activeField, "the field stays open to be corrected")
			require.Equal(t, "GET", m.method)
			require.Equal(t, "GET", m.activeRequest.Method)
			require.Nil(t, cmd, "nothing is saved for a refused method")
		})
	}
}

func TestEditMethodEscRestoresTheMethodItStartedWith(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.method = "GET"
	updated, _ := m.handleRequestAction(keybindings.ActionEditMethod)
	m = requireModel(t, updated)
	m = typeMethodKeys(t, m, "SEARCH")

	m = m.handleEsc()

	require.Equal(t, noneField, m.activeField)
	require.Equal(t, "GET", m.method)
	require.False(t, m.methodInput.Focused())
}

func TestEditMethodWithoutActiveRequestOnlySetsTheMethod(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.method = "GET"
	updated, _ := m.handleRequestAction(keybindings.ActionEditMethod)
	m = requireModel(t, updated)
	m.methodInput.SetValue("MKCOL")

	m, cmd := pressMethodEnter(t, m)

	require.Equal(t, "MKCOL", m.method)
	require.Nil(t, m.activeRequest)
	require.Nil(t, cmd)
}

func TestEditMethodDefaultKeyOpensTheInputFromTheRequestPane(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())})
	m.focus = requestPane
	m.method = "GET"

	updated, _ := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = requireModel(t, updated)

	require.Equal(t, methodField, m.activeField)
	require.Equal(t, "GET", m.methodInput.Value())
}

func TestMethodEditClickOnBadgeKeepsInputOpenAndElsewhereCancels(t *testing.T) {
	m := editorTestModel(t, nil)
	m.method = "GET"
	updated, _ := m.handleRequestAction(keybindings.ActionEditMethod)
	m = requireModel(t, updated)
	m = typeMethodKeys(t, m, "X")

	bx, by, ok := m.RequestMethodBadgeClickPos()
	require.True(t, ok)
	updated, _ = m.handleRequestClick(press(bx, by))
	m = requireModel(t, updated)
	require.Equal(t, methodField, m.activeField, "a click on the badge keeps the input open")
	require.Equal(t, "GET", m.method)

	updated, _ = m.handleRequestClick(press(0, 0))
	m = requireModel(t, updated)
	require.Equal(t, noneField, m.activeField, "a click elsewhere cancels the edit")
	require.Equal(t, "GET", m.method)
}
