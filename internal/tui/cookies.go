package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

const headerCookie = "Cookie"

// cookieJarClearer is implemented by executors that hold a session cookie
// jar. It is checked by type assertion so that executors without a jar (the
// default) simply report that nothing was cleared.
type cookieJarClearer interface {
	ClearSessionCookies() bool
}

// requestCookiePairs returns the cookies stored in a request's Cookie header
// as name/value rows, in the order they were written. A Cookie header with
// several values, or several cookies in one value, yields one row per cookie.
func requestCookiePairs(raw string) []headerPair {
	var pairs []headerPair
	for _, header := range parseHeadersJSON(raw) {
		if !strings.EqualFold(strings.TrimSpace(header.Key), headerCookie) {
			continue
		}
		for _, part := range strings.Split(header.Value, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, value, _ := strings.Cut(part, "=")
			pairs = append(pairs, headerPair{
				Key:   strings.TrimSpace(name),
				Value: strings.TrimSpace(value),
			})
		}
	}
	return pairs
}

// beginCookiesEdit opens the header editor on the request's cookies. Rows
// are name/value pairs; saving rewrites only the Cookie header.
func (m Model) beginCookiesEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	if !headersJSONValid(m.activeRequest.Headers) {
		return m.status("error",
			"This request's stored headers are not valid JSON; refusing to edit and overwrite them")
	}
	m.headerPairs = requestCookiePairs(m.activeRequest.Headers)
	m.headerCursor = 0
	m.headerEditing = false
	m.cookieEditing = true
	m.activeField = headersField
	return m.clearStatus()
}

// saveCookies writes the cookie rows back as the request's Cookie header,
// keeping every other stored header as it was.
func (m Model) saveCookies() (Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	headers, err := domain.ParseHeaders(m.activeRequest.Headers)
	if err != nil {
		return m.status("error", "Failed to read stored headers: "+err.Error()), nil
	}
	for key := range headers {
		if strings.EqualFold(strings.TrimSpace(key), headerCookie) {
			delete(headers, key)
		}
	}
	parts := make([]string, 0, len(m.headerPairs))
	for _, p := range m.headerPairs {
		name := strings.TrimSpace(p.Key)
		if name == "" {
			continue
		}
		parts = append(parts, name+"="+strings.TrimSpace(p.Value))
	}
	if len(parts) > 0 {
		headers[headerCookie] = []string{strings.Join(parts, "; ")}
	}
	encoded, err := json.Marshal(headers)
	if err != nil {
		return m.status("error", "Failed to marshal headers: "+err.Error()), nil
	}
	m.activeRequest.Headers = string(encoded)
	m.headerPairs = nil
	m.cookieEditing = false
	m.activeField = noneField
	m = m.status("success", "Cookies updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// clearSessionCookies empties the session cookie jar, if the executor has one.
func (m Model) clearSessionCookies() Model {
	clearer, ok := m.executor.(cookieJarClearer)
	if !ok || !clearer.ClearSessionCookies() {
		return m.status("error",
			"No session cookie jar: set http.cookie_jar = true in config.toml")
	}
	return m.status("success", "Session cookies cleared")
}

// renderSetCookies lists the cookies a response sets above its raw headers,
// so the Set-Cookie values are readable without parsing them by eye.
func (m Model) renderSetCookies(headers http.Header) string {
	cookies := exec.ResponseCookies(headers)
	if len(cookies) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(m.styles.info).Render(
		fmt.Sprintf("  Cookies set by response (%d)", len(cookies)),
	))
	sb.WriteString("\n")
	for _, c := range cookies {
		line := "  " + stripANSI(c.Name) + "=" + stripANSI(c.Value)
		if attrs := setCookieAttributes(c); len(attrs) > 0 {
			line += "  [" + strings.Join(attrs, ", ") + "]"
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(m.styles.foreground).Render(line))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func setCookieAttributes(c *http.Cookie) []string {
	var attrs []string
	if c.Domain != "" {
		attrs = append(attrs, "domain="+c.Domain)
	}
	if c.Path != "" {
		attrs = append(attrs, "path="+c.Path)
	}
	if !c.Expires.IsZero() {
		attrs = append(attrs, "expires="+c.Expires.UTC().Format("2006-01-02 15:04:05Z"))
	}
	if c.MaxAge > 0 {
		attrs = append(attrs, fmt.Sprintf("max-age=%d", c.MaxAge))
	}
	if c.Secure {
		attrs = append(attrs, "secure")
	}
	if c.HttpOnly {
		attrs = append(attrs, "httponly")
	}
	switch c.SameSite {
	case http.SameSiteLaxMode:
		attrs = append(attrs, "samesite=lax")
	case http.SameSiteStrictMode:
		attrs = append(attrs, "samesite=strict")
	case http.SameSiteNoneMode:
		attrs = append(attrs, "samesite=none")
	}
	return attrs
}
