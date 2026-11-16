package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

func editorTestModel(t *testing.T, req *domain.Request) Model {
	t.Helper()
	m := New(Deps{Config: config.Default(t.TempDir())})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = requireModel(t, updated)
	if req == nil {
		req = &domain.Request{
			ID:      "req-1",
			Method:  "POST",
			URL:     "https://example.com/items",
			Headers: `{"Content-Type":"application/json"}`,
			Body:    "{\"a\":1}",
		}
	}
	m.urlInput.SetValue(req.URL)
	return m.WithActiveRequest(req)
}

func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func TestRequestContentTypeHelper(t *testing.T) {
	assert.Equal(t, "", requestContentType(nil))
	assert.Equal(t, "application/json", requestContentType(&domain.Request{
		Headers: `{"content-type":"application/json"}`,
	}))
	assert.Equal(t, "", requestContentType(&domain.Request{Headers: `{"Accept":"*/*"}`}))
}

func TestUrlCursorPosFromClick(t *testing.T) {
	m := editorTestModel(t, nil)
	layout := m.currentLayout()

	m.activeField = urlField
	m.urlInput.SetValue("https://example.com")
	assert.Equal(t, 5, m.urlCursorPosFromClick(5, layout))

	m.activeField = noneField
	pos := m.urlCursorPosFromClick(3, layout)
	assert.GreaterOrEqual(t, pos, 0)
	assert.LessOrEqual(t, pos, len(m.urlInput.Value()))

	// Far-right clicks clamp to the end of the value.
	assert.Equal(t, len(m.urlInput.Value()), m.urlCursorPosFromClick(999, layout))
}

func TestHandleURLLineClickAndDoubleClickSelectsWord(t *testing.T) {
	m := editorTestModel(t, nil)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)

	m.activeField = noneField
	updated, cmd := m.handleURLLineClick(press(x, y))
	m = requireModel(t, updated)
	assert.NotNil(t, cmd)
	assert.Equal(t, urlField, m.activeField)

	// Second click on the same cell within the window counts as a double click.
	updated, _ = m.handleURLLineClick(press(x, y))
	m = requireModel(t, updated)
	assert.True(t, m.urlSelection.Active(), "double click selects the word at cursor")

	// Clicking when the URL field is already active only refocuses it.
	m.activeField = urlField
	updated, cmd = m.handleURLLineClick(press(x-50, y))
	assert.NotNil(t, cmd)
	assert.Equal(t, urlField, requireModel(t, updated).activeField)

	// Clicks left of the URL text clamp to column zero.
	m = editorTestModel(t, nil)
	m.activeField = noneField
	updated, _ = m.handleURLLineClick(press(0, y))
	assert.Equal(t, urlField, requireModel(t, updated).activeField)
}

func TestHandleHeaderListClick(t *testing.T) {
	m := editorTestModel(t, nil)
	m.headerPairs = []headerPair{{Key: "Accept", Value: "*/*"}, {Key: "X-Id", Value: "7"}}
	ll := m.requestPaneLineLayout(m.currentLayout())

	updated, _ := m.handleHeaderListClick(press(5, ll.editorContentY+1))
	got := requireModel(t, updated)
	assert.Equal(t, 1, got.headerCursor)
	assert.True(t, got.headerEditing)

	updated, _ = m.handleHeaderListClick(press(5, ll.editorContentY+9))
	assert.False(t, requireModel(t, updated).headerEditing, "click below the rows is ignored")

	updated, _ = m.handleHeaderListClick(press(5, ll.editorContentY-1))
	assert.False(t, requireModel(t, updated).headerEditing, "click above the rows is ignored")

	empty := editorTestModel(t, nil)
	updated, cmd := empty.handleHeaderListClick(press(5, 5))
	assert.Nil(t, cmd)
	assert.False(t, requireModel(t, updated).headerEditing)
}

func TestHandleHeaderEditClick(t *testing.T) {
	m := editorTestModel(t, nil)
	m.activeField = headersField
	m.headerPairs = []headerPair{{Key: "Accept", Value: "*/*"}}
	m.headerCursor = 0
	updated, _ := m.beginHeaderPairEdit(m.headerPairs[0])
	m = updated
	ll := m.requestPaneLineLayout(m.currentLayout())
	require.NotZero(t, ll.headerKeyInputY)
	content := m.currentLayout().requestContentRect()

	m2, cmd := m.handleHeaderEditClick(press(content.left+3, ll.headerKeyInputY))
	assert.NotNil(t, cmd)
	assert.True(t, m2.headerKeyInput.Focused())
	assert.False(t, m2.headerValueInput.Focused())

	m3, cmd := m.handleHeaderEditClick(press(content.left+3, ll.headerValInputY))
	assert.NotNil(t, cmd)
	assert.True(t, m3.headerValueInput.Focused())
	assert.False(t, m3.headerKeyInput.Focused())

	// Clicks left of the content column clamp to zero.
	m4, _ := m.handleHeaderEditClick(press(0, ll.headerValInputY))
	assert.True(t, m4.headerValueInput.Focused())

	m5, cmd := m.handleHeaderEditClick(press(content.left, ll.editorContentY+20))
	assert.Nil(t, cmd)
	assert.Equal(t, m.headerKeyInput.Value(), m5.headerKeyInput.Value())

	notEditing := editorTestModel(t, nil)
	_, cmd = notEditing.handleHeaderEditClick(press(3, 3))
	assert.Nil(t, cmd)
}

func TestHandleAuthEditClick(t *testing.T) {
	req := &domain.Request{
		ID:       "req-auth",
		Method:   "GET",
		URL:      "https://example.com",
		AuthType: domain.AuthTypeBearer,
	}
	m := editorTestModel(t, req)
	m.activeField = authField
	m.authEditor = newAuthEditor(req)

	rows := m.authEditor.rows()
	require.NotEmpty(t, rows)
	ll := m.requestPaneLineLayout(m.currentLayout())
	content := m.currentLayout().requestContentRect()

	// Clicking the first row (auth type, non-editable) selects it without editing.
	updated, cmd := m.handleAuthEditClick(press(content.left+3, ll.editorContentY+3))
	assert.Nil(t, cmd)
	assert.Equal(t, 0, updated.authEditor.cursor)

	// Clicking an editable row starts editing that row.
	for idx, row := range rows {
		if !authRowEditable(row) {
			continue
		}
		x, y, ok := m.AuthRowClickPos(idx)
		require.True(t, ok)
		updated, cmd = m.handleAuthEditClick(press(x, y))
		assert.NotNil(t, cmd)
		assert.Equal(t, idx, updated.authEditor.cursor)
		break
	}

	// Clicks that land outside the rows change nothing.
	updated, cmd = m.handleAuthEditClick(press(content.left, ll.editorContentY+40))
	assert.Nil(t, cmd)
	assert.Equal(t, m.authEditor.cursor, updated.authEditor.cursor)

	// Clicks left of the label clamp to column zero.
	for idx, row := range rows {
		if authRowEditable(row) {
			_, y, _ := m.AuthRowClickPos(idx)
			updated, cmd = m.handleAuthEditClick(press(0, y))
			assert.NotNil(t, cmd)
			break
		}
	}
}

func TestAuthRowHelpers(t *testing.T) {
	assert.False(t, authRowEditable(authRowType))
	assert.True(t, authRowEditable(authRowToken))
	assert.True(t, authRowEditable(authRowAPIKeyValue))
	assert.Greater(t, authRowTextLeft(10, authRowToken), 10)

	e := &authEditor{}
	in := e.inputForRow(authRowType)
	assert.Equal(t, "", in.Value())
	e.assignInput(authRowType, in) // no-op for non-editable rows must not panic

	e.inputForRow(authRowUsername)
	e.assignInput(authRowUsername, e.inputForRow(authRowUsername))
	e.assignInput(authRowPassword, e.inputForRow(authRowPassword))
	e.assignInput(authRowAPIKeyName, e.inputForRow(authRowAPIKeyName))
	e.assignInput(authRowAPIKeyValue, e.inputForRow(authRowAPIKeyValue))
	e.assignInput(authRowToken, e.inputForRow(authRowToken))
}

func TestBodyTextareaClickPositionsCursor(t *testing.T) {
	m := editorTestModel(t, nil)
	m.activeField = bodyField
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetWidth(60)
	m.bodyTextarea.SetValue("first\nsecond\nthird")
	m.bodyTextarea.Focus()

	x, y, ok := m.BodyTextareaClickPos(1, 3)
	require.True(t, ok)
	updated := m.handleBodyTextareaClick(press(x, y))
	assert.Equal(t, 1, updated.bodyTextarea.Line())

	// Click on the first logical line moves the cursor back up.
	x, y, ok = m.BodyTextareaClickPos(0, 1)
	require.True(t, ok)
	updated = updated.handleBodyTextareaClick(press(x, y))
	assert.Equal(t, 0, updated.bodyTextarea.Line())

	// Rows beyond the textarea height and clicks left of the text are ignored.
	before := updated.bodyTextarea.Line()
	updated = updated.handleBodyTextareaClick(
		press(0, updated.requestPaneLineLayout(updated.currentLayout()).editorContentY+30),
	)
	assert.Equal(t, before, updated.bodyTextarea.Line())
	updated = updated.handleBodyTextareaClick(press(0, y))
	assert.Equal(t, before, updated.bodyTextarea.Line())
}

func TestPositionBodyCursorAtDisplayLineEdgeCases(t *testing.T) {
	m := editorTestModel(t, nil)
	ta := textarea.New()
	m.bodyTextarea = ta

	m.bodyTextarea.SetWidth(10)
	m.bodyTextarea.SetValue("short\n" + strings.Repeat("x", 40))
	assert.False(t, m.positionBodyCursorAtDisplayLine(0, 0), "wrapped line fails closed")

	m.bodyTextarea.SetWidth(60)
	m.bodyTextarea.SetValue("alpha\nbeta")
	assert.False(t, m.positionBodyCursorAtDisplayLine(9, 0), "line past end is not found")
	assert.True(t, m.positionBodyCursorAtDisplayLine(0, 2))
	assert.Equal(t, 0, m.bodyTextarea.Line())
}

func TestNavigateBodyToLogicalLine(t *testing.T) {
	m := editorTestModel(t, nil)
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetWidth(60)
	m.bodyTextarea.SetValue("a\nb\nc\nd")

	m.navigateBodyToLogicalLine(2)
	assert.Equal(t, 2, m.bodyTextarea.Line())

	m.navigateBodyToLogicalLine(0)
	assert.Equal(t, 0, m.bodyTextarea.Line())

	m.navigateBodyToLogicalLine(3)
	assert.Equal(t, 3, m.bodyTextarea.Line())
}

func TestBodyTextareaTextLeftIsPastGutter(t *testing.T) {
	m := editorTestModel(t, nil)
	layout := m.currentLayout()
	assert.Equal(t, layout.requestContentRect().left+textareaPromptGutter+textareaLineNumberW,
		m.bodyTextareaTextLeft(layout))
}

func TestURLClickCollapsesSelection(t *testing.T) {
	m := editorTestModel(t, nil)
	m.activeField = urlField
	m.urlInput.SetValue("https://example.com")
	m.urlSelection.SelectRange(0, 5)
	require.True(t, m.urlSelection.Active())

	updated, _ := m.handleURLLineClick(press(999, 5))
	got, ok := updated.(Model)
	require.True(t, ok)
	assert.False(t, got.urlSelection.Active(), "a click drops the old selection")
	assert.Equal(t, len([]rune("https://example.com")), got.urlSelection.Cursor)
}

func shiftPress(x, y int) tea.MouseMsg {
	msg := press(x, y)
	msg.Shift = true
	return msg
}

func TestShiftClickExtendsURLSelection(t *testing.T) {
	m := editorTestModel(t, nil)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)

	updated, _ := m.handleURLLineClick(press(x, y))
	m = requireModel(t, updated)
	require.Equal(t, urlField, m.activeField)
	start := m.urlInput.Position()
	m.lastTextClickSource = "" // the next click must not count as a double click

	updated, _ = m.Update(shiftPress(x+8, y))
	m = requireModel(t, updated)
	assert.True(t, m.urlSelection.Active(), "shift+click selects from the old cursor")
	assert.Equal(t, start, m.urlSelection.Anchor)
	assert.Equal(t, start+8, m.urlSelection.Cursor)

	// A second shift+click moves only the cursor end; the anchor stays.
	updated, _ = m.Update(shiftPress(x+3, y))
	m = requireModel(t, updated)
	assert.Equal(t, start, m.urlSelection.Anchor)
	assert.Equal(t, start+3, m.urlSelection.Cursor)

	// A plain click collapses it again.
	updated, _ = m.Update(press(x, y))
	m = requireModel(t, updated)
	assert.False(t, m.urlSelection.Active())
}

func TestShiftClickOutsideEditedTextIsIgnored(t *testing.T) {
	m := editorTestModel(t, nil)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)
	m.activeField = noneField

	updated, cmd := m.Update(shiftPress(x, y))
	got := requireModel(t, updated)
	assert.Nil(t, cmd)
	assert.Equal(t, noneField, got.activeField, "shift+click must not start editing")
}

func TestShiftClickExtendsBodySelection(t *testing.T) {
	m := editorTestModel(t, &domain.Request{ID: "r", Method: "POST", URL: "u", Body: "hello world"})
	m = m.WithActiveField(bodyField).WithBodyEdit()
	layout := m.currentLayout()
	ll := m.requestPaneLineLayout(layout)
	left := m.bodyTextareaTextLeft(layout)
	m.bodyTextarea.SetCursor(0)
	m.bodySelection.Collapse(0)

	updated, _ := m.Update(shiftPress(left+5, ll.editorContentY))
	m = requireModel(t, updated)
	assert.True(t, m.bodySelection.Active())
	assert.Equal(t, 0, m.bodySelection.Anchor)
	assert.Equal(t, 5, m.bodySelection.Cursor)
}

func TestShiftClickExtendsHeaderAndAuthSelection(t *testing.T) {
	m := editorTestModel(t, nil)
	m = m.WithHeaderPairEdit("Authorization", "")
	m.activeField = headersField
	layout := m.currentLayout()
	ll := m.requestPaneLineLayout(layout)
	left := layout.requestContentRect().left
	m.headerKeyInput.SetCursor(0)
	m.headerKeySelection.Collapse(0)

	updated, _ := m.Update(shiftPress(left+6, ll.headerKeyInputY))
	got := requireModel(t, updated)
	assert.Equal(t, 0, got.headerKeySelection.Anchor)
	assert.Equal(t, 6, got.headerKeySelection.Cursor)

	// A shift+click on the other input only moves focus; nothing is extended.
	updated, _ = got.Update(shiftPress(left+2, ll.headerValInputY))
	got = requireModel(t, updated)
	assert.False(t, got.headerValueSelection.Active())

	a := editorTestModel(t, nil).WithAuthFieldEdit("bearer", 1, "secrettoken")
	layout = a.currentLayout()
	ll = a.requestPaneLineLayout(layout)
	a.authEditor.tokenInput.SetCursor(0)
	a.authEditor.tokenSelection.Collapse(0)
	x := authRowTextLeft(layout.requestContentRect().left, authRowToken) + 4
	updated, _ = a.Update(shiftPress(x, ll.editorContentY+3+1))
	a = requireModel(t, updated)
	assert.Equal(t, 0, a.authEditor.tokenSelection.Anchor)
	assert.Equal(t, 4, a.authEditor.tokenSelection.Cursor)
}

func TestShiftClickBodyMultiline(t *testing.T) {
	body := "{\n  \"name\": \"quark\",\n  \"id\": 12345\n}"
	m := editorTestModel(t, &domain.Request{ID: "r", Method: "POST", URL: "u", Body: body})
	m = m.WithActiveField(bodyField).WithBodyEdit()
	layout := m.currentLayout()
	ll := m.requestPaneLineLayout(layout)
	left := m.bodyTextareaTextLeft(layout)

	updated, _ := m.Update(shiftPress(left+5, ll.editorContentY+1))
	m = requireModel(t, updated)
	assert.True(t, m.bodySelection.Active())
}

// A wrapped row above the click used to make body clicks fail closed, so
// neither plain nor shift clicks did anything on such a body.
func TestBodyClickHonoursWrappedRows(t *testing.T) {
	body := strings.Repeat("w", 200) + "\nsecond line here\nthird line here"
	m := editorTestModel(t, &domain.Request{ID: "r", Method: "POST", URL: "u", Body: body})
	m = m.WithActiveField(bodyField).WithBodyEdit()
	layout := m.currentLayout()
	ll := m.requestPaneLineLayout(layout)
	left := m.bodyTextareaTextLeft(layout)
	require.Equal(t, 0, strings.Index(m.bodyTextarea.View(), "┃")) // sanity: view rendered

	rows := len(wrapRunes([]rune(strings.Repeat("w", 200)), m.bodyTextarea.Width()))
	require.Greater(t, rows, 1, "the first line must wrap")

	// Plain click on "second line" (the row after the wrapped line).
	updated, _ := m.Update(press(left+3, ll.editorContentY+rows))
	got := requireModel(t, updated)
	assert.Equal(t, 201+3, textedit.Area(&got.bodyTextarea).Cursor())

	// Shift+click on the third line extends from there.
	updated, _ = got.Update(shiftPress(left+5, ll.editorContentY+rows+1))
	got = requireModel(t, updated)
	assert.Equal(t, 201+3, got.bodySelection.Anchor)
	assert.Equal(t, 201+17+5, got.bodySelection.Cursor)
}

func dragTo(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
}

// Press, then move with the left button held: the selection grows from the
// press position, in the URL line and in the body.
func TestDragSelectsInURLAndBody(t *testing.T) {
	m := editorTestModel(t, nil)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)
	updated, _ := m.Update(press(x, y))
	m = requireModel(t, updated)
	start := m.urlInput.Position()
	m.lastTextClickSource = ""
	for _, dx := range []int{2, 5, 9} {
		updated, _ = m.Update(dragTo(x+dx, y))
		m = requireModel(t, updated)
	}
	assert.Equal(t, start, m.urlSelection.Anchor)
	assert.Equal(t, start+9, m.urlSelection.Cursor)

	req := &domain.Request{ID: "r", Method: "POST", URL: "u", Body: "hello world\nsecond"}
	b := editorTestModel(t, req)
	b = b.WithActiveField(bodyField).WithBodyEdit()
	layout := b.currentLayout()
	ll := b.requestPaneLineLayout(layout)
	left := b.bodyTextareaTextLeft(layout)
	updated, _ = b.Update(press(left+1, ll.editorContentY))
	b = requireModel(t, updated)
	updated, _ = b.Update(dragTo(left+4, ll.editorContentY+1))
	b = requireModel(t, updated)
	assert.Equal(t, 1, b.bodySelection.Anchor)
	assert.Equal(t, 12+4, b.bodySelection.Cursor)
}

// Mouse reports that lose their escape byte must not be typed into the body.
func TestLeakedMouseReportsAreNotTyped(t *testing.T) {
	b := editorTestModel(t, &domain.Request{ID: "r", Method: "POST", URL: "u", Body: "ab"})
	b = b.WithActiveField(bodyField).WithBodyEdit()
	leak := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<32;49;8M[<32;48;6M")}
	updated, _ := b.Update(leak)
	assert.Equal(t, "ab", requireModel(t, updated).BodyValue())

	// Ordinary typing and pastes still work.
	updated, _ = b.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.Contains(t, requireModel(t, updated).BodyValue(), "x")
}

// A fast drag can leave a lone "[" or a piece such as "<32;4" in the key stream.
// Typed over a selection it would replace the text, so pieces that follow a
// mouse event are dropped, and only then.
func TestStrayMouseReportPiecesDoNotReplaceSelection(t *testing.T) {
	now := time.Unix(1000, 0)
	m := editorTestModel(t, &domain.Request{ID: "r", Method: "POST", URL: "u", Body: "hello world"})
	m.now = func() time.Time { return now }
	m = m.WithActiveField(bodyField).WithBodyEdit()
	m.bodySelection.SelectRange(0, 5)

	updated, _ := m.Update(dragTo(1, 1))
	m = requireModel(t, updated)
	for _, piece := range []string{"[", "<32;4", "8M"} {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(piece)})
		m = requireModel(t, updated)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("["), Alt: true})
	m = requireModel(t, updated)
	assert.Equal(t, "hello world", m.BodyValue())

	// Long after the last mouse event, "[" is ordinary typing again.
	now = now.Add(time.Second)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	assert.Contains(t, requireModel(t, updated).BodyValue(), "[")
}
