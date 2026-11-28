package tui_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// writeFakeEditor creates a shell script that records its arguments and the
// file it was given, then replaces the file's contents with newBody.
func writeFakeEditor(t *testing.T, newBody string) (script, argsLog, seenLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake editor is a shell script")
	}
	dir := t.TempDir()
	argsLog = filepath.Join(dir, "args.log")
	seenLog = filepath.Join(dir, "seen.log")
	bodyFile := filepath.Join(dir, "new-body")
	require.NoError(t, os.WriteFile(bodyFile, []byte(newBody), 0o600))
	script = filepath.Join(dir, "fake editor.sh")
	content := "#!/bin/sh\n" +
		"for last; do :; done\n" +
		"printf '%s\\n' \"$@\" > '" + argsLog + "'\n" +
		"cat \"$last\" > '" + seenLog + "'\n" +
		"cat '" + bodyFile + "' > \"$last\"\n"
	require.NoError(t, os.WriteFile(script, []byte(content), 0o700)) //nolint:gosec // test script
	return script, argsLog, seenLog
}

func externalEditorModel(t *testing.T, editor, contentType, body string) tui.Model {
	t.Helper()
	cfg := defaultConfig()
	cfg.UI.Editor = editor
	headers := "{}"
	if contentType != "" {
		headers = `{"Content-Type":"` + contentType + `"}`
	}
	req := &domain.Request{
		ID: "req-1", Method: "POST", URL: "http://x", Headers: headers, Body: body,
	}
	m := newModel(cfg).WithSyncExecProcess().
		WithFocus(tui.RequestPane).
		WithActiveRequest(req)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	require.Equal(t, tui.BodyField, m.ActiveField())
	return m
}

func runExternalEditor(t *testing.T, m tui.Model) tui.Model {
	t.Helper()
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.NotNil(t, cmd, "ctrl+o should start the external editor")
	return callUpdate(t, m, cmd())
}

func TestExternalEditorLoadsEditedBodyWithoutSaving(t *testing.T) {
	script, argsLog, seenLog := writeFakeEditor(t, `{"edited":true}`)
	// Quote the path (it has a space) and pass an extra argument, like `code --wait`.
	m := externalEditorModel(t, `"`+script+`" --wait`, "application/json", `{"a":1}`)

	m = runExternalEditor(t, m)

	assert.Equal(t, `{"edited":true}`, m.BodyValue())
	assert.Equal(t, tui.BodyField, m.ActiveField(), "body editor stays open")
	assert.Equal(t, `{"a":1}`, m.ActiveRequest().Body, "edit is not saved until the user saves")
	assert.Contains(t, m.StatusSuccess(), "Body loaded from editor")

	seen, err := os.ReadFile(seenLog)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(seen), "editor receives the current body")

	argsData, err := os.ReadFile(argsLog)
	require.NoError(t, err)
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	require.Len(t, args, 2)
	assert.Equal(t, "--wait", args[0])
	assert.True(t, strings.HasSuffix(args[1], ".json"), "JSON bodies get a .json file: %s", args[1])
	_, statErr := os.Stat(args[1]) //nolint:gosec // path written by the test's own script
	assert.True(t, os.IsNotExist(statErr), "temp file is removed")

	// Saving afterwards stores the edited body.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.JSONEq(t, `{"edited":true}`, m.ActiveRequest().Body)
}

func TestExternalEditorUsesTextExtensionForPlainBodies(t *testing.T) {
	script, argsLog, _ := writeFakeEditor(t, "plain")
	m := externalEditorModel(t, `'`+script+`'`, "text/plain", "hello")

	m = runExternalEditor(t, m)

	assert.Equal(t, "plain", m.BodyValue())
	argsData, err := os.ReadFile(argsLog)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(strings.TrimSpace(string(argsData)), ".txt"))
}

func TestExternalEditorFailureKeepsBody(t *testing.T) {
	m := externalEditorModel(t, "false", "", "keep me")

	m = runExternalEditor(t, m)

	assert.Equal(t, "keep me", m.BodyValue())
	assert.Contains(t, m.StatusErr(), "External editor")
}

func TestExternalEditorEmptyCommand(t *testing.T) {
	m := externalEditorModel(t, "   ", "", "body")

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})

	assert.Nil(t, cmd)
	assert.Contains(t, m.StatusErr(), "no editor configured")
}

func TestExternalEditorUnterminatedQuote(t *testing.T) {
	m := externalEditorModel(t, `"vim`, "", "body")

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})

	assert.Nil(t, cmd)
	assert.Contains(t, m.StatusErr(), "unterminated quote")
}

func TestExternalEditorResultIgnoredAfterLeavingBodyEditor(t *testing.T) {
	script, _, _ := writeFakeEditor(t, "new")
	m := externalEditorModel(t, `"`+script+`"`, "", "old")

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = callUpdate(t, m, cmd())

	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "old", m.ActiveRequest().Body)
}
