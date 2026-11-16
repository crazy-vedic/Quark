package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBeginAuthEditRequiresActiveRequest(t *testing.T) {
	m := editorTestModel(t, nil).WithActiveRequest(nil)
	m = m.beginAuthEdit()
	assert.Equal(t, "Select a request first", m.StatusErr())
	assert.NotEqual(t, authField, m.activeField)

	m = editorTestModel(t, nil)
	m = m.beginAuthEdit()
	assert.Equal(t, authField, m.activeField)
}

func TestCancelActiveFieldEditRestoresValues(t *testing.T) {
	m := editorTestModel(t, nil)

	m.activeField = urlField
	m.preEditURL = "https://original.example"
	m.urlInput.SetValue("https://changed.example")
	m = m.cancelActiveFieldEdit()
	assert.Equal(t, noneField, m.activeField)
	assert.Equal(t, "https://original.example", m.urlInput.Value())

	m.activeField = bodyField
	m.preEditBody = "original body"
	m.bodyTextarea.SetValue("changed body")
	m = m.cancelActiveFieldEdit()
	assert.Equal(t, noneField, m.activeField)
	assert.Equal(t, "original body", m.bodyTextarea.Value())

	m.activeField = authField
	m.authEditor = newAuthEditor(m.activeRequest)
	m = m.cancelActiveFieldEdit()
	assert.Equal(t, noneField, m.activeField)
	assert.Equal(t, authEditor{}, m.authEditor)

	m.activeField = headersField
	m.headerPairs = []headerPair{{Key: "a", Value: "b"}}
	m.headerEditing = true
	m = m.cancelActiveFieldEdit()
	assert.Equal(t, noneField, m.activeField)
	assert.Nil(t, m.headerPairs)
	assert.False(t, m.headerEditing)

	m.activeField = noneField
	before := m
	m = m.cancelActiveFieldEdit()
	assert.Equal(t, before.activeField, m.activeField)
}

func TestFinishURLEditCommitsAndPersists(t *testing.T) {
	m := editorTestModel(t, nil).WithActiveRequest(nil)
	m.activeField = urlField
	updated, cmd := m.finishURLEdit()
	assert.Nil(t, cmd, "no active request means nothing to save")
	assert.Equal(t, noneField, updated.activeField)

	m = editorTestModel(t, nil)
	m.activeField = urlField
	m.urlInput.SetValue("  https://trimmed.example/x  ")
	updated, cmd = m.finishURLEdit()
	assert.NotNil(t, cmd)
	assert.Equal(t, noneField, updated.activeField)
	require.NotNil(t, updated.activeRequest)
	assert.Equal(t, "https://trimmed.example/x", updated.activeRequest.URL)
}

func TestURLCursorPosFromClickNarrowLayoutFloorsWidth(t *testing.T) {
	m := editorTestModel(t, nil)
	m.activeField = noneField
	m.urlInput.SetValue("https://example.com/a/very/long/path/value")
	pos := m.urlCursorPosFromClick(4, normalLayout{mainW: 5})
	assert.GreaterOrEqual(t, pos, 0)
	assert.LessOrEqual(t, pos, len(m.urlInput.Value()))
}

func TestHandleBodyTextareaClickIgnoresWrappedLines(t *testing.T) {
	m := editorTestModel(t, nil)
	m.activeField = bodyField
	m.bodyTextarea = textarea.New()
	m.bodyTextarea.SetWidth(10)
	m.bodyTextarea.SetValue(strings.Repeat("w", 40))
	m.bodyTextarea.Focus()

	x, y, ok := m.BodyTextareaClickPos(0, 2)
	require.True(t, ok)
	updated := m.handleBodyTextareaClick(press(x, y))
	assert.Equal(t, 0, updated.bodyTextarea.Line())
	assert.Equal(t, m.bodyTextarea.Value(), updated.bodyTextarea.Value())
}
