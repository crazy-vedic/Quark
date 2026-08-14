package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/crazy-vedic/quark/internal/domain"
)

func newAuthTextInput() textinput.Model {
	in := textinput.New()
	return in
}

func newAuthEditor(req *domain.Request) authEditor {
	ed := authEditor{
		authType:         domain.NormalizeAuthType(req.AuthType),
		apiKeyIn:         domain.AuthAPIKeyInHeader,
		tokenInput:       newAuthTextInput(),
		usernameInput:    newAuthTextInput(),
		passwordInput:    newAuthTextInput(),
		apiKeyNameInput:  newAuthTextInput(),
		apiKeyValueInput: newAuthTextInput(),
	}
	cfg, err := domain.ParseAuthConfig(req.AuthConfig)
	if err == nil {
		ed.tokenInput.SetValue(cfg.Token)
		ed.usernameInput.SetValue(cfg.Username)
		ed.passwordInput.SetValue(cfg.Password)
		ed.apiKeyNameInput.SetValue(cfg.Name)
		ed.apiKeyValueInput.SetValue(cfg.Value)
		if cfg.In != "" {
			ed.apiKeyIn = domain.NormalizeAPIKeyIn(cfg.In)
		}
	}
	return ed
}

func (e authEditor) rows() []authRowID {
	rows := []authRowID{authRowType}
	switch e.authType {
	case domain.AuthTypeBearer:
		rows = append(rows, authRowToken)
	case domain.AuthTypeBasic:
		rows = append(rows, authRowUsername, authRowPassword)
	case domain.AuthTypeAPIKey:
		rows = append(rows, authRowAPIKeyIn, authRowAPIKeyName, authRowAPIKeyValue)
	}
	return rows
}

func (e *authEditor) clampCursor() {
	rows := e.rows()
	if len(rows) == 0 {
		e.cursor = 0
		return
	}
	if e.cursor < 0 {
		e.cursor = 0
	}
	if e.cursor >= len(rows) {
		e.cursor = len(rows) - 1
	}
}

func (e authEditor) currentRow() authRowID {
	rows := e.rows()
	if len(rows) == 0 {
		return authRowType
	}
	if e.cursor < 0 || e.cursor >= len(rows) {
		return rows[0]
	}
	return rows[e.cursor]
}

func (e *authEditor) cycleType(delta int) {
	types := []string{
		domain.AuthTypeNone,
		domain.AuthTypeBearer,
		domain.AuthTypeBasic,
		domain.AuthTypeAPIKey,
	}
	cur := 0
	for i, authType := range types {
		if authType == e.authType {
			cur = i
			break
		}
	}
	cur = (cur + delta + len(types)) % len(types)
	e.authType = types[cur]
	e.clampCursor()
}

func (e *authEditor) cycleAPIKeyIn(delta int) {
	opts := []string{domain.AuthAPIKeyInHeader, domain.AuthAPIKeyInQuery}
	cur := 0
	for i, opt := range opts {
		if opt == e.apiKeyIn {
			cur = i
			break
		}
	}
	cur = (cur + delta + len(opts)) % len(opts)
	e.apiKeyIn = opts[cur]
}

func (e *authEditor) beginEdit() {
	e.editing = true
	switch e.currentRow() {
	case authRowToken:
		e.tokenInput.Focus()
	case authRowUsername:
		e.usernameInput.Focus()
	case authRowPassword:
		e.passwordInput.Focus()
	case authRowAPIKeyName:
		e.apiKeyNameInput.Focus()
	case authRowAPIKeyValue:
		e.apiKeyValueInput.Focus()
	default:
		e.editing = false
	}
}

func (e *authEditor) endEdit() {
	e.editing = false
	e.tokenInput.Blur()
	e.usernameInput.Blur()
	e.passwordInput.Blur()
	e.apiKeyNameInput.Blur()
	e.apiKeyValueInput.Blur()
}

func (e authEditor) config() domain.AuthConfig {
	switch e.authType {
	case domain.AuthTypeBearer:
		return domain.AuthConfig{Token: e.tokenInput.Value()}
	case domain.AuthTypeBasic:
		return domain.AuthConfig{
			Username: e.usernameInput.Value(),
			Password: e.passwordInput.Value(),
		}
	case domain.AuthTypeAPIKey:
		return domain.AuthConfig{
			In:    e.apiKeyIn,
			Name:  e.apiKeyNameInput.Value(),
			Value: e.apiKeyValueInput.Value(),
		}
	default:
		return domain.AuthConfig{}
	}
}

func (e authEditor) summary() string {
	switch e.authType {
	case domain.AuthTypeBearer:
		return "Bearer"
	case domain.AuthTypeBasic:
		return "Basic"
	case domain.AuthTypeAPIKey:
		name := strings.TrimSpace(e.apiKeyNameInput.Value())
		if name == "" {
			return "API Key"
		}
		return fmt.Sprintf("API Key (%s:%s)", e.apiKeyIn, name)
	default:
		return "None"
	}
}

func (e authEditor) valueForRow(row authRowID, editing bool) string {
	switch row {
	case authRowType:
		switch e.authType {
		case domain.AuthTypeBearer:
			return "Bearer"
		case domain.AuthTypeBasic:
			return "Basic"
		case domain.AuthTypeAPIKey:
			return "API Key"
		default:
			return "None"
		}
	case authRowToken:
		if editing && e.tokenInput.Focused() {
			return e.tokenInput.View()
		}
		if e.tokenInput.Value() == "" {
			return mutedStyle.Render("(empty)")
		}
		return "[REDACTED]"
	case authRowUsername:
		if editing && e.usernameInput.Focused() {
			return e.usernameInput.View()
		}
		if e.usernameInput.Value() == "" {
			return mutedStyle.Render("(empty)")
		}
		return e.usernameInput.Value()
	case authRowPassword:
		if editing && e.passwordInput.Focused() {
			return e.passwordInput.View()
		}
		if e.passwordInput.Value() == "" {
			return mutedStyle.Render("(empty)")
		}
		return "[REDACTED]"
	case authRowAPIKeyIn:
		if e.apiKeyIn == domain.AuthAPIKeyInQuery {
			return "Query"
		}
		return "Header"
	case authRowAPIKeyName:
		if editing && e.apiKeyNameInput.Focused() {
			return e.apiKeyNameInput.View()
		}
		if e.apiKeyNameInput.Value() == "" {
			return mutedStyle.Render("(empty)")
		}
		return e.apiKeyNameInput.Value()
	case authRowAPIKeyValue:
		if editing && e.apiKeyValueInput.Focused() {
			return e.apiKeyValueInput.View()
		}
		if e.apiKeyValueInput.Value() == "" {
			return mutedStyle.Render("(empty)")
		}
		return "[REDACTED]"
	default:
		return ""
	}
}

// singleLineValue returns the underlying value for warning/validation UI.
// valueForRow intentionally redacts secrets, so it must not be used for length
// checks on auth fields.
func (e authEditor) singleLineValue(row authRowID) string {
	switch row {
	case authRowToken:
		return e.tokenInput.Value()
	case authRowUsername:
		return e.usernameInput.Value()
	case authRowPassword:
		return e.passwordInput.Value()
	case authRowAPIKeyName:
		return e.apiKeyNameInput.Value()
	case authRowAPIKeyValue:
		return e.apiKeyValueInput.Value()
	default:
		return ""
	}
}

func authRowLabel(row authRowID) string {
	switch row {
	case authRowType:
		return "Type"
	case authRowToken:
		return "Token"
	case authRowUsername:
		return "Username"
	case authRowPassword:
		return "Password"
	case authRowAPIKeyIn:
		return "Location"
	case authRowAPIKeyName:
		return "Key"
	case authRowAPIKeyValue:
		return "Value"
	default:
		return ""
	}
}

func marshalAuthConfig(cfg domain.AuthConfig) string {
	//nolint:gosec // Request auth configs intentionally serialize password fields.
	b, _ := json.Marshal(cfg)
	return string(b)
}
