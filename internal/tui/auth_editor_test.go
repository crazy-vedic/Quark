package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestAuthEditor_Digest(t *testing.T) {
	req := &domain.Request{
		ID:         "req-digest",
		Method:     "GET",
		URL:        "https://example.com",
		AuthType:   domain.AuthTypeDigest,
		AuthConfig: `{"username":"alice","password":"s3cret"}`,
	}
	ed := newAuthEditor(req)
	assert.Equal(t, domain.AuthTypeDigest, ed.authType)
	assert.Equal(t, []authRowID{authRowType, authRowUsername, authRowPassword}, ed.rows())
	assert.Equal(t, "Digest", ed.summary())
	muted := lipgloss.NewStyle()
	assert.Equal(t, "Digest", ed.valueForRow(authRowType, false, muted))
	assert.Equal(t, "alice", ed.valueForRow(authRowUsername, false, muted))
	assert.Equal(t, "[REDACTED]", ed.valueForRow(authRowPassword, false, muted))
	assert.Equal(t,
		domain.AuthConfig{Username: "alice", Password: "s3cret"}, ed.config())

	// Digest sits after API Key in the cycle and wraps back to None.
	ed.authType = domain.AuthTypeAPIKey
	ed.cycleType(1)
	assert.Equal(t, domain.AuthTypeDigest, ed.authType)
	ed.cycleType(1)
	assert.Equal(t, domain.AuthTypeNone, ed.authType)
	ed.cycleType(-1)
	assert.Equal(t, domain.AuthTypeDigest, ed.authType)
}

func TestAuthEditor_DigestSavesToRequest(t *testing.T) {
	req := &domain.Request{ID: "req-digest", Method: "GET", URL: "https://example.com"}
	m := editorTestModel(t, req)
	m.activeField = authField
	m.authEditor = newAuthEditor(req)
	m.authEditor.cycleType(-1) // None -> Digest (last in the cycle)
	require.Equal(t, domain.AuthTypeDigest, m.authEditor.authType)
	m.authEditor.usernameInput.SetValue("bob")
	m.authEditor.passwordInput.SetValue("pw")

	m = m.finishAuthEdit()
	assert.Equal(t, domain.AuthTypeDigest, m.activeRequest.AuthType)
	cfg, err := domain.ParseAuthConfig(m.activeRequest.AuthConfig)
	require.NoError(t, err)
	assert.Equal(t, "bob", cfg.Username)
	assert.Equal(t, "pw", cfg.Password)
}
