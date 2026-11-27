package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// redactedText stands in for a secret in the editor and the request pane.
const redactedText = "[REDACTED]"

func newAuthTextInput() textinput.Model {
	in := textinput.New()
	return in
}

func newAuthEditor(req *domain.Request) authEditor {
	ed := authEditor{
		authType:          domain.NormalizeAuthType(req.AuthType),
		apiKeyIn:          domain.AuthAPIKeyInHeader,
		tokenInput:        newAuthTextInput(),
		usernameInput:     newAuthTextInput(),
		passwordInput:     newAuthTextInput(),
		apiKeyNameInput:   newAuthTextInput(),
		apiKeyValueInput:  newAuthTextInput(),
		authURLInput:      newAuthTextInput(),
		tokenURLInput:     newAuthTextInput(),
		clientIDInput:     newAuthTextInput(),
		clientSecretInput: newAuthTextInput(),
		scopeInput:        newAuthTextInput(),
	}
	cfg, err := domain.ParseAuthConfig(req.AuthConfig)
	if err == nil {
		ed.tokenInput.SetValue(cfg.Token)
		ed.usernameInput.SetValue(cfg.Username)
		ed.passwordInput.SetValue(cfg.Password)
		ed.apiKeyNameInput.SetValue(cfg.Name)
		ed.apiKeyValueInput.SetValue(cfg.Value)
		ed.authURLInput.SetValue(cfg.AuthURL)
		ed.tokenURLInput.SetValue(cfg.TokenURL)
		ed.clientIDInput.SetValue(cfg.ClientID)
		ed.clientSecretInput.SetValue(cfg.ClientSecret)
		ed.scopeInput.SetValue(cfg.Scope)
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
	case domain.AuthTypeOAuth2ClientCredentials:
		rows = append(rows, authRowTokenURL, authRowClientID, authRowClientSecret, authRowScope)
	case domain.AuthTypeOAuth2AuthorizationCode:
		rows = append(rows, authRowAuthURL, authRowTokenURL, authRowClientID,
			authRowClientSecret, authRowScope)
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
		domain.AuthTypeOAuth2ClientCredentials,
		domain.AuthTypeOAuth2AuthorizationCode,
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
	case authRowAuthURL:
		e.authURLInput.Focus()
	case authRowTokenURL:
		e.tokenURLInput.Focus()
	case authRowClientID:
		e.clientIDInput.Focus()
	case authRowClientSecret:
		e.clientSecretInput.Focus()
	case authRowScope:
		e.scopeInput.Focus()
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
	e.authURLInput.Blur()
	e.tokenURLInput.Blur()
	e.clientIDInput.Blur()
	e.clientSecretInput.Blur()
	e.scopeInput.Blur()
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
	case domain.AuthTypeOAuth2ClientCredentials:
		return e.oauth2Config("")
	case domain.AuthTypeOAuth2AuthorizationCode:
		return e.oauth2Config(e.authURLInput.Value())
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
	case domain.AuthTypeOAuth2ClientCredentials:
		return "OAuth2 Client Credentials"
	case domain.AuthTypeOAuth2AuthorizationCode:
		return "OAuth2 Auth Code"
	default:
		return "None"
	}
}

func (e authEditor) valueForRow(row authRowID, editing bool, muted lipgloss.Style) string {
	switch row {
	case authRowType:
		switch e.authType {
		case domain.AuthTypeBearer:
			return "Bearer"
		case domain.AuthTypeBasic:
			return "Basic"
		case domain.AuthTypeAPIKey:
			return "API Key"
		case domain.AuthTypeOAuth2ClientCredentials:
			return "OAuth2 Client Credentials"
		case domain.AuthTypeOAuth2AuthorizationCode:
			return "OAuth2 Auth Code"
		default:
			return "None"
		}
	case authRowAuthURL:
		return authTextDisplay(&e.authURLInput, editing, muted, false)
	case authRowTokenURL:
		return authTextDisplay(&e.tokenURLInput, editing, muted, false)
	case authRowClientID:
		return authTextDisplay(&e.clientIDInput, editing, muted, false)
	case authRowClientSecret:
		return authTextDisplay(&e.clientSecretInput, editing, muted, true)
	case authRowScope:
		return authTextDisplay(&e.scopeInput, editing, muted, false)
	case authRowToken:
		if editing && e.tokenInput.Focused() {
			return e.tokenInput.View()
		}
		if e.tokenInput.Value() == "" {
			return muted.Render("(empty)")
		}
		return redactedText
	case authRowUsername:
		if editing && e.usernameInput.Focused() {
			return e.usernameInput.View()
		}
		if e.usernameInput.Value() == "" {
			return muted.Render("(empty)")
		}
		return e.usernameInput.Value()
	case authRowPassword:
		if editing && e.passwordInput.Focused() {
			return e.passwordInput.View()
		}
		if e.passwordInput.Value() == "" {
			return muted.Render("(empty)")
		}
		return redactedText
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
			return muted.Render("(empty)")
		}
		return e.apiKeyNameInput.Value()
	case authRowAPIKeyValue:
		if editing && e.apiKeyValueInput.Focused() {
			return e.apiKeyValueInput.View()
		}
		if e.apiKeyValueInput.Value() == "" {
			return muted.Render("(empty)")
		}
		return redactedText
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
	case authRowAuthURL:
		return e.authURLInput.Value()
	case authRowTokenURL:
		return e.tokenURLInput.Value()
	case authRowClientID:
		return e.clientIDInput.Value()
	case authRowClientSecret:
		return e.clientSecretInput.Value()
	case authRowScope:
		return e.scopeInput.Value()
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
	case authRowAuthURL:
		return "Auth URL"
	case authRowTokenURL:
		return "Token URL"
	case authRowClientID:
		return "Client ID"
	case authRowClientSecret:
		return "Client Secret"
	case authRowScope:
		return "Scope"
	default:
		return ""
	}
}

func marshalAuthConfig(cfg domain.AuthConfig) string {
	//nolint:gosec // Request auth configs intentionally serialize password fields.
	b, _ := json.Marshal(cfg)
	return string(b)
}

// activeInput returns the selection and widget for a text row. Other rows
// return nil.
func (e *authEditor) activeInput(row authRowID) (*textedit.Selection, *textinput.Model) {
	switch row {
	case authRowToken:
		return &e.tokenSelection, &e.tokenInput
	case authRowUsername:
		return &e.usernameSelection, &e.usernameInput
	case authRowPassword:
		return &e.passwordSelection, &e.passwordInput
	case authRowAPIKeyName:
		return &e.apiKeyNameSelection, &e.apiKeyNameInput
	case authRowAPIKeyValue:
		return &e.apiKeyValueSelection, &e.apiKeyValueInput
	case authRowAuthURL:
		return &e.authURLSelection, &e.authURLInput
	case authRowTokenURL:
		return &e.tokenURLSelection, &e.tokenURLInput
	case authRowClientID:
		return &e.clientIDSelection, &e.clientIDInput
	case authRowClientSecret:
		return &e.clientSecretSelection, &e.clientSecretInput
	case authRowScope:
		return &e.scopeSelection, &e.scopeInput
	}
	return nil, nil
}

// oauth2Config builds the stored OAuth 2.0 config. authURL is empty for client
// credentials, which has no authorization step.
func (e authEditor) oauth2Config(authURL string) domain.AuthConfig {
	return domain.AuthConfig{
		AuthURL:      authURL,
		TokenURL:     e.tokenURLInput.Value(),
		ClientID:     e.clientIDInput.Value(),
		ClientSecret: e.clientSecretInput.Value(),
		Scope:        e.scopeInput.Value(),
	}
}

// authTextDisplay renders a text row the way the other auth rows do. Secrets
// are redacted when not being edited.
func authTextDisplay(in *textinput.Model, editing bool, muted lipgloss.Style, secret bool) string {
	if editing && in.Focused() {
		return in.View()
	}
	if in.Value() == "" {
		return muted.Render("(empty)")
	}
	if secret {
		return redactedText
	}
	return in.Value()
}
