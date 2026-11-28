//go:build e2e

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

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

// writeExternalEditorScript writes a POSIX shell "editor" that records its
// arguments and the file it was handed, then replaces that file with newBody.
// The script lives in a directory whose name contains a space, so callers can
// exercise quoting and escaping in the configured editor command.
func writeExternalEditorScript(t *testing.T, newBody string) (script, argsLog, seenLog string) {
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

// externalEditorJourney opens the body editor on a request with the given
// editor command and Content-Type, using a synchronous process runner.
func externalEditorJourney(t *testing.T, editor, contentType, body string) tui.Model {
	t.Helper()
	cfg := config.Default(t.TempDir())
	cfg.UI.Editor = editor
	headers := "{}"
	if contentType != "" {
		headers = `{"Content-Type":"` + contentType + `"}`
	}
	req := &domain.Request{
		ID: "ext-editor-req", Name: "Editor request", Method: "POST",
		URL: "https://editor.example", Headers: headers, Body: body,
	}
	st := setupStore(t)
	m := newE2EModelWithConfig(t, st, &mockExecutor{}, cfg).
		WithSyncExecProcess().
		WithFocus(tui.RequestPane).
		WithActiveRequest(req)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	require.Equal(t, tui.BodyField, m.ActiveField())
	return m
}

func openExternalEditor(t *testing.T, m tui.Model) tui.Model {
	t.Helper()
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.NotNil(t, cmd, "ctrl+o must start the external editor")
	return callUpdate(t, m, cmd())
}

// The edited file is loaded back into the body editor, the temp file is
// removed, and the request is not saved until the user saves the body.
func TestE2E_ExternalEditor_QuotedCommandLoadsEditedBody(t *testing.T) {
	script, argsLog, seenLog := writeExternalEditorScript(t, `{"edited":true}`)
	m := externalEditorJourney(t, `"`+script+`" --wait`, "application/json", `{"a":1}`)

	m = openExternalEditor(t, m)

	assert.Equal(t, `{"edited":true}`, m.BodyValue())
	assert.Equal(t, tui.BodyField, m.ActiveField(), "body editor stays open after the editor exits")
	assert.Equal(t, `{"a":1}`, m.ActiveRequest().Body, "edits are not saved until the body is saved")
	assert.Equal(t, "Body loaded from editor (not saved yet)", m.StatusSuccess())

	seen, err := os.ReadFile(seenLog)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(seen), "editor receives the current body")

	args := readEditorArgs(t, argsLog)
	require.Len(t, args, 2)
	assert.Equal(t, "--wait", args[0], "extra editor flags come before the file")
	assert.True(t, strings.HasSuffix(args[1], ".json"), "JSON bodies get a .json file: %s", args[1])
	_, statErr := os.Stat(args[1]) //nolint:gosec // path written by the test's own script
	assert.True(t, os.IsNotExist(statErr), "temp file is removed after the editor exits")
}

// Single quotes group a path with spaces; backslash escapes a space outside
// quotes. Both must reach the editor as one program name.
func TestE2E_ExternalEditor_SingleQuotedAndEscapedPathsResolve(t *testing.T) {
	script, argsLog, _ := writeExternalEditorScript(t, "plain")
	m := externalEditorJourney(t, `'`+script+`'`, "text/plain", "hello")
	m = openExternalEditor(t, m)
	assert.Equal(t, "plain", m.BodyValue())
	args := readEditorArgs(t, argsLog)
	require.Len(t, args, 1)
	assert.True(t, strings.HasSuffix(args[0], ".txt"), "plain text bodies get a .txt file: %s", args[0])

	escaped := strings.ReplaceAll(script, " ", `\ `)
	m = externalEditorJourney(t, escaped, "text/plain", "hello")
	m = openExternalEditor(t, m)
	assert.Equal(t, "plain", m.BodyValue())
}

// Each Content-Type family gets the matching syntax extension for the temp file.
func TestE2E_ExternalEditor_ContentTypeSelectsTempFileExtension(t *testing.T) {
	cases := map[string]string{
		"application/json":    ".json",
		"application/xml":     ".xml",
		"text/html":           ".html",
		"application/yaml":    ".yaml",
		"application/graphql": ".graphql",
		"text/plain":          ".txt",
	}
	for contentType, ext := range cases {
		t.Run(contentType, func(t *testing.T) {
			script, argsLog, _ := writeExternalEditorScript(t, "edited")
			m := externalEditorJourney(t, `"`+script+`"`, contentType, "body")
			m = openExternalEditor(t, m)
			assert.Equal(t, "edited", m.BodyValue())
			args := readEditorArgs(t, argsLog)
			require.Len(t, args, 1)
			assert.True(t, strings.HasSuffix(args[0], ext), "%s must use %s: got %s", contentType, ext, args[0])
		})
	}
}

// A non-zero editor exit keeps the body as it was and reports the failure.
func TestE2E_ExternalEditor_FailedEditorKeepsBody(t *testing.T) {
	m := externalEditorJourney(t, "false", "", "keep me")
	m = openExternalEditor(t, m)
	assert.Equal(t, "keep me", m.BodyValue())
	assert.Contains(t, m.StatusErr(), "External editor")
}

// A missing editor command is reported before anything is started.
func TestE2E_ExternalEditor_ConfigurationErrors(t *testing.T) {
	m := externalEditorJourney(t, "   ", "", "body")
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	assert.Nil(t, cmd)
	assert.Equal(t, "External editor: no editor configured: set ui.editor or $EDITOR", m.StatusErr())

	m = externalEditorJourney(t, `"vim`, "", "body")
	m, cmd = callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	assert.Nil(t, cmd)
	assert.Contains(t, m.StatusErr(), "unterminated quote")

	// A trailing backslash is kept literally; the program then cannot start.
	m = externalEditorJourney(t, `no-such-editor-quark\`, "", "body")
	m = openExternalEditor(t, m)
	assert.Contains(t, m.StatusErr(), "External editor")
	assert.Equal(t, "body", m.BodyValue())
}

// If the user leaves the body editor before the editor exits, the edited text
// is dropped instead of overwriting the field they moved to.
func TestE2E_ExternalEditor_ResultIgnoredAfterLeavingBody(t *testing.T) {
	script, _, _ := writeExternalEditorScript(t, "new")
	m := externalEditorJourney(t, `"`+script+`"`, "", "old")

	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	require.NotNil(t, cmd)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = callUpdate(t, m, cmd())

	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, "old", m.ActiveRequest().Body)
}

func readEditorArgs(t *testing.T, argsLog string) []string {
	t.Helper()
	data, err := os.ReadFile(argsLog)
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}
