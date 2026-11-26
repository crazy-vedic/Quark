package tui

import (
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// externalEditorDoneMsg reports that the external editor process exited.
type externalEditorDoneMsg struct {
	requestID string
	path      string
	err       error
}

// execProcessFunc runs cmd with the terminal handed over to it and reports
// the result through callback. Production uses tea.ExecProcess; tests swap in
// a synchronous runner.
type execProcessFunc func(cmd *osexec.Cmd, callback tea.ExecCallback) tea.Cmd

// splitEditorCommand splits an editor command such as `code --wait` or
// `"/opt/My Editor/bin/edit" -n` into the program and its arguments. Single
// and double quotes group words; a backslash escapes the next character
// outside single quotes.
func splitEditorCommand(command string) ([]string, error) {
	var (
		words   []string
		current strings.Builder
		inWord  bool
		quote   rune
		escaped bool
	)
	for _, r := range command {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
			inWord = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case unicode.IsSpace(r):
			if inWord {
				words = append(words, current.String())
				current.Reset()
				inWord = false
			}
		default:
			current.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in editor command %q", command)
	}
	if escaped {
		current.WriteRune('\\')
	}
	if inWord {
		words = append(words, current.String())
	}
	if len(words) == 0 {
		return nil, errors.New("no editor configured: set ui.editor or $EDITOR")
	}
	return words, nil
}

// bodyFileExtension picks a temp-file extension from the request's
// Content-Type so the editor applies the right syntax highlighting.
func bodyFileExtension(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case isJSONContentType(contentType):
		return ".json"
	case strings.Contains(ct, "xml"):
		return ".xml"
	case strings.Contains(ct, "html"):
		return ".html"
	case strings.Contains(ct, "yaml"):
		return ".yaml"
	case strings.Contains(ct, "graphql"):
		return ".graphql"
	default:
		return ".txt"
	}
}

// openBodyInExternalEditor writes the body being edited to a temp file and
// opens it in the configured editor. The edited text is loaded back into the
// body editor when the editor exits; it is saved only when the user saves
// the body editor, exactly like typed text.
func (m Model) openBodyInExternalEditor() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	args, err := splitEditorCommand(m.cfg.UI.Editor)
	if err != nil {
		return m.status("error", "External editor: "+err.Error()), nil
	}
	ext := bodyFileExtension(requestContentType(m.activeRequest))
	f, err := os.CreateTemp("", "quark-body-*"+ext)
	if err != nil {
		return m.status("error", "External editor: "+err.Error()), nil
	}
	path := f.Name()
	_, writeErr := f.WriteString(m.bodyTextarea.Value())
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return m.status("error", "External editor: "+err.Error()), nil
	}

	// The editor is the user's own configured command.
	cmd := osexec.Command( //nolint:gosec // user-configured editor
		args[0], append(args[1:], path)...,
	)
	requestID := m.activeRequest.ID
	run := m.execProcess
	if run == nil {
		run = tea.ExecProcess
	}
	return m, run(cmd, func(err error) tea.Msg {
		return externalEditorDoneMsg{requestID: requestID, path: path, err: err}
	})
}

// handleExternalEditorDone loads the edited file back into the body editor
// and removes the temp file.
func (m Model) handleExternalEditorDone(msg externalEditorDoneMsg) Model {
	data, readErr := os.ReadFile(msg.path)
	_ = os.Remove(msg.path)
	if msg.err != nil {
		return m.status("error", "External editor: "+msg.err.Error())
	}
	if readErr != nil {
		return m.status("error", "External editor: "+readErr.Error())
	}
	if m.activeField != bodyField || m.activeRequest == nil || m.activeRequest.ID != msg.requestID {
		// The user left the body editor (or switched request) meanwhile.
		return m
	}
	m.bodyTextarea.SetValue(string(data))
	m.bodySelection.Collapse(0)
	m.newTextEditor().Sync(&m.bodySelection, textedit.Area(&m.bodyTextarea))
	return m.status("success", "Body loaded from editor (not saved yet)")
}
