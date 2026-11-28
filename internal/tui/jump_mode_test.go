package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// jumpFixture is a 120x40 model with one expanded collection holding one
// request, a second collapsed collection, and a response with a body.
func jumpFixture(t *testing.T) Model {
	t.Helper()
	return jumpFixtureWith(t, New(Deps{}))
}

func jumpFixtureWith(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = requireModel(t, updated)
	m.collections = []*domain.Collection{{ID: "col-1", Name: "Alpha"}, {ID: "col-2", Name: "Beta"}}
	m.collectionRequests = map[string][]*domain.Request{
		"col-1": {{
			ID: "r1", CollectionID: "col-1", Name: "List", Method: "GET",
			URL: "https://example.com",
		}},
	}
	m.expanded["col-1"] = true
	m.activeRequest = m.collectionRequests["col-1"][0]
	m.response = &exec.ExecuteResult{StatusCode: 200, Status: "200 OK", Body: []byte(`{"a":1}`)}
	return m
}

func pressRune(t *testing.T, m Model, r rune) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return requireModel(t, updated)
}

func pressEsc(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	return requireModel(t, updated)
}

// labelOf returns the label of the first target that matches want.
func labelOf(t *testing.T, m Model, want func(jumpTarget) bool) string {
	t.Helper()
	for _, tg := range m.jump.targets {
		if want(tg) {
			return tg.label
		}
	}
	require.Fail(t, "no jump target matches")
	return ""
}

func TestJumpModeOpensWithKeyAndLabelsTargets(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	assert.Equal(t, jumpMode, m.mode)

	actions := map[string]bool{}
	sidebarRows := 0
	for _, tg := range m.jump.targets {
		actions[tg.action] = true
		if tg.sidebar {
			sidebarRows++
		}
	}
	for _, want := range []string{
		"focus_sidebar", "focus_request", "focus_response",
		"edit_url", "edit_body", "tab_body", "tab_headers", "tab_raw",
	} {
		assert.True(t, actions[want], "missing target %s", want)
	}
	assert.Equal(t, 3, sidebarRows, "two collections and one request are visible")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "jump - type a label, esc cancels")
	assert.Contains(t, view, "╭a")
}

func TestJumpModeLabelFocusesPaneAndExits(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	label := labelOf(t, m, func(tg jumpTarget) bool { return tg.action == "focus_response" })

	m = pressRune(t, m, rune(label[0]))
	assert.Equal(t, normalMode, m.Mode())
	assert.Equal(t, responsePane, m.Focus())
	assert.Empty(t, m.jump.targets)
}

func TestJumpModeEscCancelsWithoutMoving(t *testing.T) {
	m := jumpFixture(t)
	m = pressRune(t, m, 'g')
	m = pressEsc(t, m)
	assert.Equal(t, normalMode, m.Mode())
	assert.Equal(t, sidebarPane, m.Focus())
	assert.Empty(t, m.jump.targets)
}

func TestJumpModeIgnoresKeysThatStartNoLabel(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	m = pressRune(t, m, '1')
	m = pressRune(t, m, '?')
	assert.Equal(t, jumpMode, m.Mode())
	assert.Equal(t, sidebarPane, m.Focus())
	assert.Empty(t, m.jump.typed)
}

func TestJumpModeURLLabelStartsURLEdit(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	label := labelOf(t, m, func(tg jumpTarget) bool { return tg.action == "edit_url" })
	m = pressRune(t, m, rune(label[0]))
	assert.Equal(t, normalMode, m.Mode())
	assert.Equal(t, requestPane, m.Focus())
	assert.Equal(t, urlField, m.activeField)
}

func TestJumpModeResponseTabLabelSelectsTab(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	label := labelOf(t, m, func(tg jumpTarget) bool { return tg.action == "tab_headers" })
	m = pressRune(t, m, rune(label[0]))
	assert.Equal(t, responsePane, m.Focus())
	assert.Equal(t, headersTab, m.ResponseTab())
}

func TestJumpModeSidebarRequestLabelOpensRequest(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	label := labelOf(t, m, func(tg jumpTarget) bool {
		return tg.sidebar && tg.row.kind == sidebarRequestRow
	})
	m = pressRune(t, m, rune(label[0]))
	assert.Equal(t, normalMode, m.Mode())
	assert.Equal(t, requestPane, m.Focus())
	assert.Equal(t, 0, m.ReqCursor())
	require.NotNil(t, m.activeRequest)
	assert.Equal(t, "r1", m.activeRequest.ID)
}

func TestJumpModeSidebarCollectionLabelSelectsCollection(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	label := labelOf(t, m, func(tg jumpTarget) bool {
		return tg.sidebar && tg.row.kind == sidebarCollectionRow && tg.row.colIndex == 1
	})
	m = pressRune(t, m, rune(label[0]))
	assert.Equal(t, sidebarPane, m.Focus())
	assert.Equal(t, 1, m.ColCursor())
	assert.Equal(t, -1, m.ReqCursor())
}

func TestJumpModeTwoLetterLabelsNeedBothKeys(t *testing.T) {
	m := jumpFixture(t)
	m.jump = jumpState{targets: assignJumpLabels(make([]jumpTarget, len(jumpAlphabet)+4))}
	m.mode = jumpMode
	for i := range m.jump.targets {
		m.jump.targets[i].pane = responsePane
		m.jump.targets[i].action = "focus_response"
	}
	last := m.jump.targets[len(m.jump.targets)-1]
	require.Len(t, last.label, 2)

	m = pressRune(t, m, rune(last.label[0]))
	assert.Equal(t, jumpMode, m.Mode(), "the first letter alone does not act")
	assert.Equal(t, string(last.label[0]), m.jump.typed)
	assert.Contains(t, ansi.Strip(m.View()), "jump "+string(last.label[0]))

	m = pressRune(t, m, rune(last.label[1]))
	assert.Equal(t, normalMode, m.Mode())
	assert.Equal(t, responsePane, m.Focus())
}

func TestJumpModeBackspaceClearsTypedLetter(t *testing.T) {
	m := jumpFixture(t)
	m.jump = jumpState{targets: assignJumpLabels(make([]jumpTarget, len(jumpAlphabet)+1))}
	m.mode = jumpMode
	m = pressRune(t, m, rune(m.jump.targets[len(m.jump.targets)-1].label[0]))
	require.NotEmpty(t, m.jump.typed)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = requireModel(t, updated)
	assert.Empty(t, m.jump.typed)
	assert.Equal(t, jumpMode, m.Mode())
}

func TestJumpModeNothingToJumpToReportsStatus(t *testing.T) {
	// Without a size there is no screen, so no target is on it.
	m := New(Deps{})
	m = m.openJumpMode()
	assert.Equal(t, normalMode, m.Mode())
	assert.Equal(t, "Nothing to jump to", m.StatusErr())
}

func TestJumpModeResizeCancels(t *testing.T) {
	m := pressRune(t, jumpFixture(t), 'g')
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = requireModel(t, updated)
	assert.Equal(t, normalMode, m.Mode())
	assert.Empty(t, m.jump.targets)
}

func TestJumpModeUsesConfiguredKey(t *testing.T) {
	cfg := config.Default("")
	cfg.Keybindings.JumpMode = "ctrl+t"
	m := jumpFixtureWith(t, New(Deps{Config: cfg}))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = requireModel(t, updated)
	assert.Equal(t, jumpMode, m.Mode())
}

func TestAssignJumpLabels(t *testing.T) {
	t.Run("single letters when they fit, home row first", func(t *testing.T) {
		labels := assignJumpLabels(make([]jumpTarget, 3))
		got := []string{labels[0].label, labels[1].label, labels[2].label}
		assert.Equal(t, []string{"a", "s", "d"}, got)
	})

	t.Run("two letters are unique and prefix-free beyond the alphabet", func(t *testing.T) {
		labels := assignJumpLabels(make([]jumpTarget, len(jumpAlphabet)+10))
		seen := map[string]bool{}
		for _, tg := range labels {
			require.Len(t, tg.label, 2)
			assert.False(t, seen[tg.label], "duplicate label %s", tg.label)
			seen[tg.label] = true
		}
		for _, a := range labels {
			for _, b := range labels {
				if a.label != b.label {
					assert.False(t, strings.HasPrefix(b.label, a.label))
				}
			}
		}
	})

	t.Run("targets beyond the two-letter capacity are dropped", func(t *testing.T) {
		n := len(jumpAlphabet)
		labels := assignJumpLabels(make([]jumpTarget, n*n+5))
		assert.Len(t, labels, n*n)
	})
}

func TestSpliceJumpLabelKeepsStyledSides(t *testing.T) {
	line := "\x1b[31mabcdef\x1b[0m"
	out := spliceJumpLabel(line, 2, "X")
	assert.Equal(t, "abXdef", ansi.Strip(out))
	assert.Contains(t, out, "\x1b[31m")

	padded := spliceJumpLabel("ab", 4, "X")
	assert.Equal(t, "ab  X", padded)

	assert.Equal(t, "ab", spliceJumpLabel("ab", -1, "X"))
}
