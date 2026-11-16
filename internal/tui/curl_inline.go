package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// isCurlCommand reports whether the URL text is a curl command rather than a URL.
func isCurlCommand(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "curl")
}

// openCurlReplace parses a curl command pasted or entered into the URL field
// and opens the import dialog in replace mode. Confirming replaces the active
// request; cancelling leaves it unchanged. On a parse error the text stays in
// the field so the user can fix it.
func (m Model) openCurlReplace(raw string) Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first, then paste the curl command")
	}
	if m.importer == nil {
		return m.status("error", "curl importer is unavailable")
	}
	result, err := m.importer.Parse(strings.NewReader(strings.TrimSpace(raw)))
	if err != nil {
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
func pastedText(msg tea.KeyMsg, _ textedit.Keymap) (text string, ok bool) {
	if msg.Paste {
		return string(msg.Runes), true
	}
	return "", false
}

// isMultiLineCurl reports whether text is a curl command spanning several lines.
func isMultiLineCurl(text string) bool {
	return isCurlCommand(text) && strings.ContainsAny(text, "\r\n")
}
