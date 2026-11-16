package tui_test

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

func pasteFixture(value string) tui.Model {
	return newModelWithCurl().
		WithFocus(tui.RequestPane).
		WithActiveRequest(existingRequest()).
		WithURLEdit().
		WithURLValue(value)
}

// Ctrl+V reads the clipboard asynchronously. The command it returns is run
// later, after focus may have changed. These tests hold that command and
// deliver it at a different time, as the Bubble Tea loop would.

func TestPasteDroppedWhenFocusLeavesFieldBeforeRead(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "pasted", nil })
	t.Cleanup(restore)

	m, cmd := callUpdateWithCmd(t, pasteFixture("keep"), tea.KeyMsg{Type: tea.KeyCtrlV})
	require.NotNil(t, cmd)
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	before := m.URLValue()

	m = feedPastes(t, m, cmd)
	assert.Equal(t, before, m.URLValue(), "text must not land after focus left the field")
	assert.NotContains(t, m.URLValue(), "pasted")
}

func TestPasteInsertedWhenFocusUnchanged(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "pasted", nil })
	t.Cleanup(restore)

	m := callPaste(t, pasteFixture(""), tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "pasted", m.URLValue())
}

func TestOlderPasteRequestIsDropped(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "X", nil })
	t.Cleanup(restore)

	m := pasteFixture("")
	m, first := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	m, second := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})

	m = feedPastes(t, m, first)
	assert.Equal(t, "", m.URLValue(), "a superseded paste must not insert")
	m = feedPastes(t, m, second)
	assert.Equal(t, "X", m.URLValue())
}

// A paste result delivered in normal mode must not reach the global shortcut
// path. "q" would quit the app there, so the test checks that no quit follows.
func TestPasteInNormalModeNeverFiresShortcut(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "q", nil })
	t.Cleanup(restore)

	m, cmd := callUpdateWithCmd(t, pasteFixture(""), tea.KeyMsg{Type: tea.KeyCtrlV})
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	res, ok := cmd().(textedit.PasteResultMsg)
	require.True(t, ok)
	_, next := callUpdateWithCmd(t, m, res)
	if next != nil {
		_, isQuit := next().(tea.QuitMsg)
		assert.False(t, isQuit, "pasted q must not quit the app")
	}
}

// A terminal bracketed paste with no focused field is dropped, not treated as keys.
func TestBracketedPasteInNormalModeIsIgnored(t *testing.T) {
	m := newModelWithCurl().WithFocus(tui.RequestPane)
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Paste: true}
	m2, cmd := callUpdateWithCmd(t, m, key)
	assert.Nil(t, cmd, "pasted q must not quit")
	assert.Contains(t, m2.StatusErr(), "Paste ignored")
}

func TestPasteReadErrorIsReported(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) {
		return "", errors.New("no xclip")
	})
	t.Cleanup(restore)

	m := callPaste(t, pasteFixture("keep"), tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "keep", m.URLValue())
	assert.True(t, strings.Contains(m.StatusErr(), "Clipboard read failed"), m.StatusErr())
}

// Ctrl+V in normal mode (no focused field) is a no-op. Copy in normal mode is
// also a no-op, so paste does not start editing the URL. See review S4.
func TestCtrlVInNormalModeIsNoOp(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "pasted", nil })
	t.Cleanup(restore)

	m := newModelWithCurl().WithFocus(tui.RequestPane).WithActiveRequest(existingRequest())
	m2, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Nil(t, cmd)
	assert.Equal(t, "", m2.URLValue())
}

// Sub-field focus: a paste requested in one input of an editor must not land in
// a sibling input after Tab moves focus within that same editor.

func TestPasteDroppedWhenHeaderFocusMovesToValue(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "pasted", nil })
	t.Cleanup(restore)

	m := pasteFixture("").WithHeaderPairEdit("Zq-Probe", "v1")
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	require.NotNil(t, cmd)
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, "Zq-Probe", m.HeaderKeyValue(), "precondition: paste not applied yet")

	m = feedPastes(t, m, cmd)
	assert.Equal(t, "Zq-Probe", m.HeaderKeyValue())
	assert.Equal(t, "v1", m.HeaderValueValue(), "value must not receive the key's paste")
}

func TestPasteInsertedWhenHeaderFocusUnchanged(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "X", nil })
	t.Cleanup(restore)

	m := pasteFixture("").WithHeaderPairEdit("", "")
	m = callPaste(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "X", m.HeaderKeyValue())
}

func TestPasteDroppedWhenAuthFieldChangesBeforeRead(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "leaked", nil })
	t.Cleanup(restore)

	m := pasteFixture("").WithAuthFieldEdit("bearer", 1, "tok")
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	require.NotNil(t, cmd)
	// Enter ends the token edit, Down moves to the next row, Enter edits it.
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotEqual(t, 1, m.AuthCursor(), "precondition: focus moved to another auth field")

	m = feedPastes(t, m, cmd)
	assert.NotContains(t, m.AuthTokenValue(), "leaked",
		"token must not receive another field's paste")
	assert.NotContains(t, m.AuthPasswordValue(), "leaked",
		"the newly focused field must not receive it")
}

// Every text widget in tui.Model must be covered by the paste target. A widget
// that is missing from TextWidgetAccessors fails here, which means it was added
// without a focus entry, so paste could land in it after focus moves.

var (
	textinputType = reflect.TypeOf(textinput.Model{})
	textareaType  = reflect.TypeOf(textarea.Model{})
	modelPkg      = reflect.TypeOf(tui.Model{}).PkgPath()
)

// reflectWidgetPaths lists every textinput or textarea reachable from tui.Model,
// following fields of struct types defined in the tui package.
func reflectWidgetPaths() []string {
	var out []string
	var walk func(rt reflect.Type, prefix string)
	walk = func(rt reflect.Type, prefix string) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := prefix + f.Name
			switch f.Type {
			case textinputType, textareaType:
				out = append(out, name)
			default:
				if f.Type.Kind() == reflect.Struct && f.Type.PkgPath() == modelPkg {
					walk(f.Type, name+".")
				}
			}
		}
	}
	walk(reflect.TypeOf(tui.Model{}), "")
	sort.Strings(out)
	return out
}

func TestEveryTextWidgetHasPasteCoverage(t *testing.T) {
	accessors := tui.TextWidgetAccessors()
	var missing []string
	paths := reflectWidgetPaths()
	for _, p := range paths {
		if _, ok := accessors[p]; !ok {
			missing = append(missing, p)
		}
	}
	require.Empty(t, missing, "text widgets missing from TextWidgetAccessors and focusedWidgets")
	require.Len(t, accessors, len(paths))
}

// Focusing any one text widget must change the paste target, and no two widgets
// may give the same target. Otherwise a paste requested in one input could land
// in another after focus moves.
func TestFocusingEachTextWidgetChangesPasteTarget(t *testing.T) {
	baseline := tui.BlurredModelForTest().PasteTargetForTest()
	seen := map[any]string{}
	for name, acc := range tui.TextWidgetAccessors() {
		m := tui.BlurredModelForTest()
		acc(&m).Focus()
		target := m.PasteTargetForTest()
		assert.NotEqual(t, baseline, target, "focusing %s does not change the paste target", name)
		if other, dup := seen[target]; dup {
			t.Errorf("focusing %s and %s produce the same paste target", name, other)
		}
		seen[target] = name
	}
}
