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

// Two Ctrl+V presses in the same input both apply, in the order they were
// pressed, because the focus target never changed.
func TestDoublePasteInSameInputAppliesBothInOrder(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "X", nil })
	t.Cleanup(restore)

	m := pasteFixture("")
	m, first := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	m, second := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})

	m = feedPastes(t, m, first)
	assert.Equal(t, "X", m.URLValue(), "the older paste must apply when focus is unchanged")
	m = feedPastes(t, m, second)
	assert.Equal(t, "XX", m.URLValue(), "the newer paste must apply after the older one")
}

// Bubble Tea runs commands concurrently, so the second read can finish first.
// The text must still land in the order the keys were pressed.
func TestOutOfOrderPasteResultsApplyInRequestOrder(t *testing.T) {
	reads := []string{"first", "second"}
	i := 0
	restore := tui.StubClipboardForTest(func() (string, error) {
		s := reads[i]
		i++
		return s, nil
	})
	t.Cleanup(restore)

	m := pasteFixture("")
	m, c1 := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	m, c2 := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	r1, ok := c1().(textedit.PasteResultMsg)
	require.True(t, ok)
	r2, ok := c2().(textedit.PasteResultMsg)
	require.True(t, ok)

	m, _ = callUpdateWithCmd(t, m, r2)
	assert.Equal(t, "", m.URLValue(), "the later paste must wait for the earlier one")
	m, _ = callUpdateWithCmd(t, m, r1)
	assert.Equal(t, "firstsecond", m.URLValue())
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
//
// Limit: the walk follows direct fields and nested structs only. It does not
// enter slices, maps, or pointers. A widget stored in one of those would not
// be found, so it would also be missing from the guard. If a collection of
// widgets is ever added to Model, extend this walk and add the collection to
// textWidgets.
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

// routeCase opens one text widget the way a user does, pastes into it, and
// reads back its value. A widget with a routeCase is covered by the insert
// route. Every other widget must appear in untestedWidgets with a reason.
type routeCase struct {
	name    string
	open    func(*testing.T) tui.Model
	deliver func(*testing.T, tui.Model, tea.Cmd) tui.Model
	value   func(tui.Model) string
}

// deliver feeds a Ctrl+V command's result back in. Every text widget uses the
// same ticketed PasteResultMsg, so feedPastes covers them all.
func deliver(t *testing.T, m tui.Model, cmd tea.Cmd) tui.Model {
	t.Helper()
	return feedPastes(t, m, cmd)
}

var routingCases = []routeCase{
	{
		name:    "urlInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("") },
		deliver: deliver,
		value:   tui.Model.URLValue,
	},
	{
		name:    "bodyTextarea",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithBodyEdit() },
		deliver: deliver,
		value:   tui.Model.BodyValue,
	},
	{
		name:    "viewerFilter.input",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithViewerFilterEdit("") },
		deliver: deliver,
		value:   tui.Model.ViewerFilterValue,
	},
	{
		name:    "searchInput",
		open:    openSearchByKey,
		deliver: deliver,
		value:   tui.Model.SearchInputValue,
	},
	{
		name:    "importInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithImportInputEdit("") },
		deliver: deliver,
		value:   tui.Model.ImportInputValue,
	},
	{
		name:    "headerKeyInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithHeaderPairEdit("", "") },
		deliver: deliver,
		value:   tui.Model.HeaderKeyValue,
	},
	{
		name:    "headerValueInput",
		open:    openHeaderValueByTab,
		deliver: deliver,
		value:   tui.Model.HeaderValueValue,
	},
	{
		name:    "authEditor.tokenInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithAuthFieldEdit("bearer", 1, "") },
		deliver: deliver,
		value:   tui.Model.AuthTokenValue,
	},
	{
		name:    "authEditor.usernameInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithAuthFieldEdit("basic", 1, "") },
		deliver: deliver,
		value:   tui.Model.AuthUsernameValue,
	},
	{
		name:    "authEditor.apiKeyNameInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithAuthFieldEdit("api_key", 2, "") },
		deliver: deliver,
		value:   tui.Model.AuthAPIKeyNameValue,
	},
	{
		name:    "authEditor.apiKeyValueInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithAuthFieldEdit("api_key", 3, "") },
		deliver: deliver,
		value:   tui.Model.AuthAPIKeyValueValue,
	},
	{
		name:    "envEditor.editVal",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithEnvValEdit("") },
		deliver: deliver,
		value:   tui.Model.EnvValValue,
	},
	{
		name:    "authEditor.passwordInput",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithAuthFieldEdit("basic", 2, "") },
		deliver: deliver,
		value:   tui.Model.AuthPasswordValue,
	},
	{
		name:    "envEditor.editKey",
		open:    func(*testing.T) tui.Model { return pasteFixture("").WithEnvKeyEdit("") },
		deliver: deliver,
		value:   tui.Model.EnvKeyValue,
	},
}

// untestedWidgets lists every text widget that has no routeCase, with the reason.
// Adding a widget here is a visible decision: the reason must say what is missing.
// An empty reason fails TestEveryTextWidgetIsRoutedOrExplained.
//
//nolint:gosec // G101 matches widget names such as clientCertPassword; these are not credentials.
var untestedWidgets = map[string]string{
	"viewerFind":         "find bar in the response viewer; no test helper opens it",
	"importName":         "name field shown after a curl import preview; no helper opens a preview",
	"postmanPath":        "path field in the Postman import dialog; no helper opens it",
	"promptInput":        "prompt modal input; no helper opens a prompt",
	"scheduleInput":      "schedule entry field; no helper opens the scheduler",
	"clientCertHost":     "client certificate dialog, host field; no helper opens it",
	"clientCertFile":     "client certificate dialog, file field; no helper opens it",
	"clientCertType":     "client certificate dialog, type field; no helper opens it",
	"clientCertKeyFile":  "client certificate dialog, key file field; no helper opens it",
	"clientCertCAFile":   "client certificate dialog, CA file field; no helper opens it",
	"clientCertPassword": "client certificate dialog, password field; no helper opens it",
}

// TestPasteRoutesToEachRoutedWidget checks that a tagged paste lands in the
// widget's own value.
func TestPasteRoutesToEachRoutedWidget(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "R", nil })
	t.Cleanup(restore)

	for _, c := range routingCases {
		t.Run(c.name, func(t *testing.T) {
			m, cmd := callUpdateWithCmd(t, c.open(t), tea.KeyMsg{Type: tea.KeyCtrlV})
			require.NotNil(t, cmd)
			m = c.deliver(t, m, cmd)
			assert.Contains(t, c.value(m), "R", "the paste must land in %s", c.name)
		})
	}
}

// Import paste inserts at the cursor and keeps existing text. It is not a
// replace: the import dialog does not promise an empty field.
func TestImportPasteInsertsAndKeepsExistingText(t *testing.T) {
	restore := tui.StubClipboardForTest(func() (string, error) { return "R", nil })
	t.Cleanup(restore)

	m := pasteFixture("").WithImportInputEdit("curl ")
	m = callPaste(t, m, tea.KeyMsg{Type: tea.KeyCtrlV})
	assert.Equal(t, "curl R", m.ImportInputValue())
}

// Every text widget in Model is either routed or explained. A widget that is
// in neither place fails here, so a new widget cannot slip past silently.
func TestEveryTextWidgetIsRoutedOrExplained(t *testing.T) {
	// Pin the count so adding a widget to the list, or dropping one, is a
	// visible decision. Update this when a widget gains a routeCase.
	assert.Len(t, untestedWidgets, 11, "untestedWidgets changed size; update this pin deliberately")
	for name, reason := range untestedWidgets {
		assert.Greater(t, len(strings.TrimSpace(reason)), 15, "%s: reason is a placeholder", name)
	}

	routed := map[string]bool{}
	for _, c := range routingCases {
		routed[c.name] = true
	}
	for name := range tui.TextWidgetAccessors() {
		if routed[name] {
			continue
		}
		reason, ok := untestedWidgets[name]
		assert.True(t, ok, "%s has no routeCase and no untestedWidgets reason", name)
		assert.NotEmpty(t, strings.TrimSpace(reason), "%s needs a reason in untestedWidgets", name)
	}
}

// Every entry in untestedWidgets must name a real widget that is not routed.
// A stale entry, or one for a routed widget, fails here.
func TestUntestedWidgetsAreAccurate(t *testing.T) {
	accessors := tui.TextWidgetAccessors()
	routed := map[string]bool{}
	for _, c := range routingCases {
		routed[c.name] = true
	}
	for name := range untestedWidgets {
		assert.Contains(t, accessors, name, "untestedWidgets names a widget that does not exist")
		assert.False(t, routed[name], "%s is routed, so it should not be in untestedWidgets", name)
	}
}

// openSearchByKey enters search mode with the same key a user presses.
func openSearchByKey(t *testing.T) tui.Model {
	t.Helper()
	m := newModelWithCurl().WithFocus(tui.RequestPane).WithActiveRequest(existingRequest())
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	require.Equal(t, tui.SearchMode, m.Mode(), "precondition: / opens search")
	return m
}

// openHeaderValueByTab opens a header pair and moves focus to its value input.
func openHeaderValueByTab(t *testing.T) tui.Model {
	t.Helper()
	m := pasteFixture("").WithHeaderPairEdit("Zq-Probe", "")
	m, _ = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyTab})
	return m
}
