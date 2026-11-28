package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestAuthEditor_OAuth2CycleAndRows(t *testing.T) {
	ed := newAuthEditor(&domain.Request{AuthType: domain.AuthTypeAPIKey})

	ed.cycleType(1)
	assert.Equal(t, domain.AuthTypeOAuth2ClientCredentials, ed.authType)
	assert.Equal(t, []authRowID{
		authRowType, authRowTokenURL, authRowClientID, authRowClientSecret, authRowScope,
	}, ed.rows())
	assert.Equal(t, "OAuth2 Client Credentials", ed.summary())

	ed.cycleType(1)
	assert.Equal(t, domain.AuthTypeOAuth2AuthorizationCode, ed.authType)
	assert.Equal(t, []authRowID{
		authRowType,
		authRowAuthURL,
		authRowTokenURL,
		authRowClientID,
		authRowClientSecret,
		authRowScope,
	}, ed.rows())
	assert.Equal(t, "OAuth2 Auth Code", ed.summary())

	ed.cycleType(1)
	assert.Equal(t, domain.AuthTypeNone, ed.authType, "the cycle wraps back to None")
}

func TestAuthEditor_OAuth2LoadsAndRedactsSecret(t *testing.T) {
	//nolint:gosec // G101 flags the ClientSecret fixture field; this is not a credential.
	req := &domain.Request{
		AuthType: domain.AuthTypeOAuth2AuthorizationCode,
		AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
			AuthURL:      "https://auth.example.test/authorize",
			TokenURL:     "https://auth.example.test/token",
			ClientID:     "app",
			ClientSecret: "s3cret",
			Scope:        "read",
		}),
	}
	ed := newAuthEditor(req)
	muted := lipgloss.NewStyle()
	assert.Equal(t, "https://auth.example.test/authorize",
		ed.valueForRow(authRowAuthURL, false, muted))
	assert.Equal(t, "app", ed.valueForRow(authRowClientID, false, muted))
	assert.Equal(t, "[REDACTED]", ed.valueForRow(authRowClientSecret, false, muted))
	assert.Equal(t, "OAuth2 Auth Code", ed.valueForRow(authRowType, false, muted))
	assert.Equal(t, "s3cret", ed.singleLineValue(authRowClientSecret),
		"the redacted secret is still readable for validation")
}

func TestAuthEditor_OAuth2SavesConfigPerGrant(t *testing.T) {
	req := &domain.Request{ID: "req-oauth", Method: "GET", URL: "https://example.com"}
	m := editorTestModel(t, req)
	m.activeField = authField
	m.authEditor = newAuthEditor(req)
	// None -> Bearer -> Basic -> API Key -> Client Credentials.
	for range 4 {
		m.authEditor.cycleType(1)
	}
	require.Equal(t, domain.AuthTypeOAuth2ClientCredentials, m.authEditor.authType)
	m.authEditor.tokenURLInput.SetValue("https://auth.example.test/token")
	m.authEditor.clientIDInput.SetValue("app")
	m.authEditor.clientSecretInput.SetValue("s3cret")
	m.authEditor.scopeInput.SetValue("read")
	m.authEditor.authURLInput.SetValue("https://ignored.example.test/authorize")

	m = m.finishAuthEdit()
	assert.Equal(t, domain.AuthTypeOAuth2ClientCredentials, m.activeRequest.AuthType)
	cfg, err := domain.ParseAuthConfig(m.activeRequest.AuthConfig)
	require.NoError(t, err)
	assert.Empty(t, cfg.AuthURL, "client credentials has no authorization URL")
	assert.Equal(t, "https://auth.example.test/token", cfg.TokenURL)
	assert.Equal(t, "app", cfg.ClientID)
	assert.Equal(t, "s3cret", cfg.ClientSecret)
	assert.Equal(t, "read", cfg.Scope)
}

func TestAuthEditor_OAuth2AuthCodeKeepsAuthURL(t *testing.T) {
	req := &domain.Request{ID: "req-oauth-ac", Method: "GET", URL: "https://example.com"}
	m := editorTestModel(t, req)
	m.activeField = authField
	m.authEditor = newAuthEditor(req)
	m.authEditor.authType = domain.AuthTypeOAuth2AuthorizationCode
	m.authEditor.authURLInput.SetValue("https://auth.example.test/authorize")
	m.authEditor.tokenURLInput.SetValue("https://auth.example.test/token")
	m.authEditor.clientIDInput.SetValue("app")

	m = m.finishAuthEdit()
	cfg, err := domain.ParseAuthConfig(m.activeRequest.AuthConfig)
	require.NoError(t, err)
	assert.Equal(t, "https://auth.example.test/authorize", cfg.AuthURL)
	assert.Equal(t, "https://auth.example.test/token", cfg.TokenURL)
	assert.Empty(t, cfg.ClientSecret, "a public client has no secret")
}
