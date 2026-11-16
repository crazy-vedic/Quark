package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
)

// isCurlCommand reports whether the URL text is a curl command rather than a URL.
func isCurlCommand(s string) bool {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "curl") {
		return false
	}
	rest := t[len("curl"):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r'
}

// openCurlReplace parses a curl command entered into the URL field and opens
// the import dialog in replace mode. Confirming replaces the active request;
// cancelling leaves it unchanged. On a parse error the text stays in the field
// so the user can fix it.
func (m Model) openCurlReplace(raw string) Model {
	return m.openCurlReplaceFrom(raw, false)
}

// openCurlReplaceFromPaste is openCurlReplace for a bracketed paste. A paste
// never reaches the URL field, so on a parse error the import dialog opens
// pre-filled with the pasted text instead, where the user can fix it.
func (m Model) openCurlReplaceFromPaste(raw string) Model {
	return m.openCurlReplaceFrom(raw, true)
}

func (m Model) openCurlReplaceFrom(raw string, fromPaste bool) Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first, then paste the curl command")
	}
	if m.importer == nil {
		return m.status("error", "curl importer is unavailable")
	}
	result, err := m.importer.Parse(strings.NewReader(strings.TrimSpace(raw)))
	if err != nil {
		if fromPaste {
			m = m.openCurlImport()
			m.importInput.SetValue(raw)
			m.importError = "Could not parse curl: " + err.Error()
			return m
		}
		return m.status("error", "Could not parse curl: "+err.Error())
	}

	m.importPreview = result
	m.importReplace = true
	m.importError = ""
	m.mode = importMode
	m.importName.Blur()
	m.importInput.Blur()
	m.activeField = noneField
	m.urlInput.SetValue(m.preEditURL)
	m.urlInput.Blur()
	m.urlSelection.Collapse(0)
	return m
}

// confirmCurlReplace writes the previewed command into the active request,
// keeping its name and ID, and saves it.
func (m Model) confirmCurlReplace() (Model, tea.Cmd) {
	result := m.importPreview
	if result.Certificate != nil {
		m.importError = "A client certificate can't be applied to an existing request. " +
			"Cancel and use the normal import to create a new one."
		return m, nil
	}
	if m.activeRequest == nil {
		m = m.closeImport()
		return m.status("error", "No request selected"), nil
	}
	headersJSON, err := json.Marshal(result.Headers)
	if err != nil {
		m.importError = "Could not read curl headers: " + err.Error()
		return m, nil
	}

	req := m.activeRequest
	req.Method = result.Method
	req.URL = result.URL
	req.Headers = string(headersJSON)
	req.Body = result.Body
	// The curl result carries no auth settings (credentials arrive as headers),
	// so the previous request's auth must not apply to the new request.
	req.AuthType = domain.AuthTypeNone
	req.AuthConfig = ""

	m.method = result.Method
	m.preEditURL = result.URL
	m.urlInput.SetValue(result.URL)
	m = m.closeImport()
	cmd := saveRequestCmd(m.ctx, m.writer, m.reader, req)
	return m.reportCurlReplace(result), cmd
}

// reportCurlReplace shows the result in the status bar, with any parser warnings.
func (m Model) reportCurlReplace(result *curl.ImportResult) Model {
	if len(result.Warnings) == 0 {
		return m.status("success", "Replaced request from curl command")
	}
	return m.status("warn", fmt.Sprintf(
		"Replaced request from curl command with %d warning(s): %s",
		len(result.Warnings), result.Warnings[0],
	))
}

// pastedText returns the text of a bracketed paste. ok is false for any other
// key. Ctrl+V is not read here: it reads the clipboard asynchronously and
// arrives as a bracketed paste, so the UI never blocks on the clipboard.
func pastedText(msg tea.KeyMsg) (text string, ok bool) {
	if msg.Paste {
		return string(msg.Runes), true
	}
	return "", false
}
