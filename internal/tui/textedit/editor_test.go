package textedit

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct {
	text  string
	cur   int
	multi bool
}

func (f *fakeBackend) Text() string         { return f.text }
func (f *fakeBackend) SetText(s string)     { f.text = s }
func (f *fakeBackend) Cursor() int          { return f.cur }
func (f *fakeBackend) SetCursor(p int)      { f.cur = p }
func (f *fakeBackend) MultiLine() bool      { return f.multi }
func (f *fakeBackend) MoveVertical(int) int { return f.cur }
func (f *fakeBackend) Update(msg tea.Msg) tea.Cmd {
	// Mimic a widget: plain runes insert at the cursor, Backspace deletes before it.
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch km.Type {
	case tea.KeyRunes:
		r := []rune(f.text)
		ins := []rune(string(km.Runes))
		out := append(append(append([]rune{}, r[:f.cur]...), ins...), r[f.cur:]...)
		f.text = string(out)
		f.cur += len(ins)
	case tea.KeyBackspace:
		if f.cur > 0 {
			r := []rune(f.text)
			f.text = string(append(append([]rune{}, r[:f.cur-1]...), r[f.cur:]...))
			f.cur--
		}
	case tea.KeyLeft:
		if f.cur > 0 {
			f.cur--
		}
	case tea.KeyRight:
		if f.cur < len([]rune(f.text)) {
			f.cur++
		}
	}
	return nil
}

type recordingClipboard struct {
	writes  []string
	content string
	readErr error
}

func (r *recordingClipboard) ReadCmd() tea.Cmd {
	return PasteCmd(func() (string, error) { return r.content, r.readErr })
}
func (r *recordingClipboard) WriteCmd(s string) tea.Cmd {
	r.writes = append(r.writes, s)
	return nil
}

type harness struct {
	ed  Editor
	sel Selection
	b   *fakeBackend
	clp *recordingClipboard
}

func newHarness(text string, cur int) *harness {
	b := &fakeBackend{text: text, cur: cur}
	clp := &recordingClipboard{}
	h := &harness{ed: New(clp), b: b, clp: clp}
	h.sel.Collapse(cur)
	return h
}

// press sends msg and then runs any returned command and feeds its message
// back, as the Bubble Tea loop does. A paste therefore lands in the same call.
func (h *harness) press(msg tea.KeyMsg) bool {
	cmd, handled := h.ed.HandleKey(&h.sel, h.b, msg)
	if cmd != nil {
		if res, ok := cmd().(PasteResultMsg); ok && res.Err == nil {
			h.ed.HandleKey(&h.sel, h.b, pasteKey(res.Text))
		}
	}
	return handled
}

func (h *harness) key(s string) bool {
	return h.press(keyFor(s))
}

// keyFor returns the KeyMsg bubbletea reports for the key strings the tests use.
func keyFor(s string) tea.KeyMsg {
	table := map[string]tea.KeyMsg{
		"left":             {Type: tea.KeyLeft},
		"right":            {Type: tea.KeyRight},
		"home":             {Type: tea.KeyHome},
		"end":              {Type: tea.KeyEnd},
		"ctrl+left":        {Type: tea.KeyCtrlLeft},
		"ctrl+right":       {Type: tea.KeyCtrlRight},
		"shift+left":       {Type: tea.KeyShiftLeft},
		"shift+right":      {Type: tea.KeyShiftRight},
		"shift+home":       {Type: tea.KeyShiftHome},
		"shift+end":        {Type: tea.KeyShiftEnd},
		"ctrl+shift+left":  {Type: tea.KeyCtrlShiftLeft},
		"ctrl+shift+right": {Type: tea.KeyCtrlShiftRight},
		"ctrl+a":           {Type: tea.KeyCtrlA},
		"ctrl+c":           {Type: tea.KeyCtrlC},
		"ctrl+x":           {Type: tea.KeyCtrlX},
		"ctrl+v":           {Type: tea.KeyCtrlV},
		"ctrl+w":           {Type: tea.KeyCtrlW},
		"backspace":        {Type: tea.KeyBackspace},
		"enter":            {Type: tea.KeyEnter},
		"tab":              {Type: tea.KeyTab},
	}
	km, ok := table[s]
	if !ok {
		panic("unknown test key: " + s)
	}
	return km
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestShiftLeftRepeatedGrowsOneSelection(t *testing.T) {
	h := newHarness("abcdef", 6)
	for i := 0; i < 3; i++ {
		h.key("shift+left")
	}
	s, e := h.sel.Range()
	assert.Equal(t, 3, s)
	assert.Equal(t, 6, e)
}

func TestBackwardSelectionCopiesInOrder(t *testing.T) {
	h := newHarness("abcdef", 4)
	h.key("shift+left")
	h.key("shift+left")
	h.key("ctrl+c")
	assert.Equal(t, []string{"cd"}, h.clp.writes)
	assert.Equal(t, "abcdef", h.b.text)
}

func TestCtrlCWithoutSelectionIsNotHandled(t *testing.T) {
	h := newHarness("abc", 1)
	assert.False(t, h.key("ctrl+c"))
	assert.Empty(t, h.clp.writes)
}

func TestCtrlXWithoutSelectionIsNoOp(t *testing.T) {
	h := newHarness("https://example.com", 5)
	assert.True(t, h.key("ctrl+x"))
	assert.Equal(t, 5, h.b.cur)
	assert.Equal(t, "https://example.com", h.b.text)
	assert.Empty(t, h.clp.writes)
}

func TestCtrlXWithSelectionWritesThenDeletes(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.key("shift+right")
	h.key("shift+right")
	h.key("ctrl+x")
	assert.Equal(t, "cdef", h.b.text)
	assert.Equal(t, 0, h.b.cur)
	assert.Equal(t, []string{"ab"}, h.clp.writes)
}

func TestTypingReplacesSelection(t *testing.T) {
	h := newHarness("hello world", 0)
	h.key("ctrl+a")
	assert.True(t, h.press(runes("X")))
	assert.Equal(t, "X", h.b.text)
	assert.Equal(t, 1, h.b.cur)
	assert.False(t, h.sel.Active())
}

func TestBackspaceWithSelectionDeletesOnlySelection(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.key("ctrl+a")
	h.key("backspace")
	assert.Equal(t, "", h.b.text)
	h.key("shift+right") // nothing to extend over: stays at 0
	assert.Equal(t, 0, h.sel.Cursor)
}

func TestBackspaceWithoutSelectionDeletesOneChar(t *testing.T) {
	h := newHarness("abc", 2)
	h.key("backspace")
	assert.Equal(t, "ac", h.b.text)
	assert.Equal(t, 1, h.b.cur)
}

func TestLeftWithSelectionCollapsesToStartWithoutMoving(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.key("ctrl+a")
	h.key("left")
	assert.Equal(t, 0, h.b.cur)
	assert.False(t, h.sel.Active())
}

func TestRightWithSelectionCollapsesToEnd(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.key("shift+right")
	h.key("shift+right")
	h.key("right")
	assert.Equal(t, 2, h.b.cur)
	assert.False(t, h.sel.Active())
}

func TestCtrlShiftWordExtend(t *testing.T) {
	h := newHarness("one two three", 0)
	h.key("ctrl+shift+right")
	assert.Equal(t, "one ", selectedText(h.b.text, h.sel))
	h.key("ctrl+shift+right")
	assert.Equal(t, "one two ", selectedText(h.b.text, h.sel))
	h.key("ctrl+shift+left")
	assert.Equal(t, "one ", selectedText(h.b.text, h.sel))
}

func TestShiftHomeAndEnd(t *testing.T) {
	h := newHarness("abcdef", 3)
	h.key("shift+home")
	s, e := h.sel.Range()
	assert.Equal(t, [2]int{0, 3}, [2]int{s, e})
	h.key("shift+end")
	s, e = h.sel.Range()
	assert.Equal(t, [2]int{3, 6}, [2]int{s, e})
}

func TestPasteReplacesSelectionAndInsertsAtCursor(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.clp.content = "XY"
	h.key("shift+right")
	h.key("shift+right")
	assert.True(t, h.key("ctrl+v"))
	assert.Equal(t, "XYcdef", h.b.text)
	assert.Equal(t, 2, h.b.cur)
	assert.False(t, h.sel.Active())
}

func TestPasteWithoutSelectionInsertsAtCursor(t *testing.T) {
	h := newHarness("ad", 1)
	h.clp.content = "bc"
	h.key("ctrl+v")
	assert.Equal(t, "abcd", h.b.text)
	assert.Equal(t, 3, h.b.cur)
}

func TestPasteStripsNewlinesForSingleLine(t *testing.T) {
	h := newHarness("", 0)
	h.clp.content = "https://x.dev\n"
	h.key("ctrl+v")
	assert.Equal(t, "https://x.dev", h.b.text)
}

func TestPasteIsDeferredUntilClipboardReadRuns(t *testing.T) {
	h := newHarness("ad", 1)
	h.clp.content = "bc"
	cmd, handled := h.ed.HandleKey(&h.sel, h.b, keyFor("ctrl+v"))
	assert.True(t, handled)
	assert.Equal(t, "ad", h.b.text, "the UI must not read or insert synchronously")
	require.NotNil(t, cmd)
	res, ok := cmd().(PasteResultMsg)
	require.True(t, ok)
	assert.Equal(t, "bc", res.Text)
	h.ed.HandleKey(&h.sel, h.b, pasteKey(res.Text))
	assert.Equal(t, "abcd", h.b.text)
}

func TestPasteReadFailureIsDelivered(t *testing.T) {
	h := newHarness("abc", 1)
	h.clp.readErr = errors.New("no clipboard")
	cmd, _ := h.ed.HandleKey(&h.sel, h.b, keyFor("ctrl+v"))
	res, ok := cmd().(PasteResultMsg)
	require.True(t, ok)
	assert.Error(t, res.Err)
}

func TestPasteEmptyClipboardDeliversNothing(t *testing.T) {
	h := newHarness("abc", 1)
	cmd, _ := h.ed.HandleKey(&h.sel, h.b, keyFor("ctrl+v"))
	assert.Nil(t, cmd())
}

func pasteKey(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}
}

func TestPasteFailureLeavesTextAlone(t *testing.T) {
	h := newHarness("abc", 1)
	h.clp.readErr = errors.New("no clipboard")
	assert.True(t, h.key("ctrl+v"))
	assert.Equal(t, "abc", h.b.text)
}

func TestCtrlWWithSelectionRemovesOnlySelection(t *testing.T) {
	h := newHarness("one two three", 4)
	h.key("shift+right")
	h.key("shift+right")
	assert.True(t, h.key("ctrl+w"))
	assert.Equal(t, "one o three", h.b.text) // selected "tw"
}

func TestNonOwnedKeyIsNotHandled(t *testing.T) {
	h := newHarness("abc", 0)
	assert.False(t, h.key("enter"))
	assert.False(t, h.key("tab"))
}

func TestSyncFollowsWidgetCursor(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.key("shift+right")
	h.b.cur = 4 // widget moved outside the editor
	h.ed.Sync(&h.sel, h.b)
	assert.False(t, h.sel.Active())
	assert.Equal(t, 4, h.sel.Cursor)
}

func TestSelectionFromCollapsedUsesBackendCursor(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.b.cur = 5 // stale selection, backend moved by mouse click
	h.key("shift+left")
	s, e := h.sel.Range()
	assert.Equal(t, [2]int{4, 5}, [2]int{s, e})
}

func TestKeymapBindingsAreUniqueAndNonEmpty(t *testing.T) {
	k := DefaultKeymap()
	seen := map[string]string{}
	for name, b := range map[string]Binding{
		"CharLeft": k.CharLeft, "CharRight": k.CharRight, "WordLeft": k.WordLeft,
		"WordRight": k.WordRight, "LineStart": k.LineStart, "LineEnd": k.LineEnd,
		"SelectCharLeft": k.SelectCharLeft, "SelectCharRight": k.SelectCharRight,
		"SelectWordLeft": k.SelectWordLeft, "SelectWordRight": k.SelectWordRight,
		"SelectLineStart": k.SelectLineStart, "SelectLineEnd": k.SelectLineEnd,
		"SelectAll": k.SelectAll, "Copy": k.Copy, "Cut": k.Cut,
		"Paste": k.Paste, "DeleteWord": k.DeleteWord,
	} {
		assert.NotEmpty(t, b, name)
		for _, s := range b {
			assert.NotEmpty(t, s, name)
			if other, dup := seen[s]; dup && other != name {
				// alt+left/right are shared by WordLeft/WordRight only within their own action.
				t.Errorf("key %q bound to both %s and %s", s, other, name)
			}
			seen[s] = name
		}
	}
}

func TestSelectionSingleCollapseHelpers(t *testing.T) {
	var s Selection
	s.SelectRange(2, 5)
	assert.True(t, s.Active())
	s.SelectAll(7)
	assert.Equal(t, Selection{0, 7}, s)
	s.Collapse(3)
	assert.False(t, s.Active())
}

func TestWordBoundsHyphenSeparates(t *testing.T) {
	runes := []rune("Content-Type")
	start, end := WordBoundsAt(runes, 3)
	assert.Equal(t, [2]int{0, 7}, [2]int{start, end})
}

func TestSyncKeepsSelectionWhenCursorUnchanged(t *testing.T) {
	h := newHarness("abcdef", 0)
	h.key("shift+right")
	h.key("shift+right")
	h.ed.Sync(&h.sel, h.b) // e.g. a cursor blink tick: nothing moved
	s, e := h.sel.Range()
	assert.Equal(t, [2]int{0, 2}, [2]int{s, e})
}

// Enter in a single-line field is not owned by the editor. The caller handles
// it, and the selection must survive the key untouched.
func TestEnterInSingleLineFieldIsLeftToCaller(t *testing.T) {
	h := newHarness("abc", 0)
	h.b.multi = false
	h.key("shift+end")
	before := h.sel
	assert.False(t, h.press(tea.KeyMsg{Type: tea.KeyEnter}))
	assert.Equal(t, "abc", h.b.text)
	assert.Equal(t, before, h.sel)
}

// Pasting multi-line text into a single-line field turns inner newlines into
// spaces, as sanitize documents. It must not insert a stray line break.
func TestPasteMultiLineIntoSingleLineReplacesNewlinesWithSpace(t *testing.T) {
	h := newHarness("", 0)
	h.clp.content = "one\ntwo"
	h.key("ctrl+v")
	assert.Equal(t, "one two", h.b.text)
}

func TestSanitizeSingleLine(t *testing.T) {
	cases := map[string]string{
		"foo\nbar":    "foo bar",
		"foo\r\nbar":  "foo bar",
		"foo\rbar":    "foo bar",
		"\nfoo\n":     "foo",
		"\r\nfoo\r\n": "foo",
		"a\n\nb":      "a b",
		"plain":       "plain",
	}
	for in, want := range cases {
		assert.Equal(t, want, sanitize(in, false), "input %q", in)
	}
}
