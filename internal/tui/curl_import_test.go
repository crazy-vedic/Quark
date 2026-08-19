package tui

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
)

const exactIAMCurl = `curl 'https://access.dev.wealthcareadmin.com/access/connect/token' \
  --header 'Content-Type: application/x-www-form-urlencoded' \
  --cert-type P12 \
  --cert '/mnt/c/Users/vedic.varma/Downloads/dev-auth 1.p12:Password1' \
  --data-urlencode 'user_id=3n1kl3DpdbiW3OANNDq6PA==' \
  --data-urlencode 'grant_type=Cert' \
  --data-urlencode 'client_id=dapr_cron_job' \
  --data-urlencode 'scope=mbi_api offline_access bensoft_api openid profile'`

type importedRequestWriter struct{ request *domain.Request }

func (w *importedRequestWriter) SaveRequest(_ context.Context, request *domain.Request) error {
	w.request = request
	return nil
}

func (w *importedRequestWriter) DeleteRequest(context.Context, string) error { return nil }

type pasteImporter struct{ calls int }

func (p *pasteImporter) Parse(_ io.Reader) (*curl.ImportResult, error) {
	p.calls++
	return &curl.ImportResult{Method: http.MethodPost, URL: "https://example.com", Headers: make(http.Header)}, nil
}

type importedCertificateManager struct{ cfg config.Config }

func (m *importedCertificateManager) Reload(cfg config.Config) error {
	m.cfg = cfg
	return nil
}

func TestImportCurlKeyOpensDedicatedMultilineModal(t *testing.T) {
	m := New(Deps{Importer: curl.NewImporter()})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	var ok bool
	m, ok = updated.(Model)
	require.True(t, ok)

	require.Equal(t, importMode, m.mode)
	require.Nil(t, m.importPreview)
	require.True(t, m.importInput.Focused())
}

func TestURLFieldNeverAutoImportsCurl(t *testing.T) {
	importer := &pasteImporter{}
	m := New(Deps{Importer: importer})
	m.activeField = urlField
	m.urlInput.Focus()
	m.urlInput.SetValue("curl https://example.com")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Paste: true})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	require.Equal(t, normalMode, m.mode)
	require.Equal(t, 0, importer.calls)
	require.Contains(t, m.statusErr, "press I")
}

func TestCompleteClipboardValueParsesExactIAMCommand(t *testing.T) {
	m := New(Deps{Importer: curl.NewImporter()}).openCurlImport()
	updated, _ := m.Update(curlClipboardMsg{value: exactIAMCurl})
	m = updated.(Model)
	require.Equal(t, exactIAMCurl, m.importInput.Value())

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)
	require.NotNil(t, m.importPreview)
	require.Equal(t, http.MethodPost, m.importPreview.Method)
	require.Equal(t, "https://access.dev.wealthcareadmin.com/access/connect/token", m.importPreview.URL)
	require.Equal(t, "application/x-www-form-urlencoded", m.importPreview.Headers.Get("Content-Type"))
	require.Contains(t, m.importPreview.Body, "user_id=3n1kl3DpdbiW3OANNDq6PA%3D%3D")
	require.Contains(t, m.importPreview.Body, "scope=mbi_api%20offline_access")
	require.Equal(t, "P12", m.importPreview.Certificate.Type)
}

func TestImportInputEnterInsertsNewlineWithoutPartialParse(t *testing.T) {
	importer := &pasteImporter{}
	m := New(Deps{Importer: importer}).openCurlImport()
	m.importInput.SetValue("curl https://example.com \\")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	require.Nil(t, m.importPreview)
	require.Equal(t, 0, importer.calls)
	require.Contains(t, m.importInput.Value(), "\n")
}

func TestImportModalAllowsUppercaseIInCommandAndSaveName(t *testing.T) {
	m := New(Deps{Importer: curl.NewImporter()}).openCurlImport()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("curl https://example.com/I")})
	m = updated.(Model)
	require.Contains(t, m.importInput.Value(), "/I")

	m.importPreview = &curl.ImportResult{Method: http.MethodGet, URL: "https://example.com"}
	m.importName.Focus()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = updated.(Model)
	require.Equal(t, "I", m.importName.Value())
}

func TestImportConfirmationPersistsHeadersBodyAndCertificate(t *testing.T) {
	writer := &importedRequestWriter{}
	manager := &importedCertificateManager{}
	configDir := t.TempDir()
	m := New(Deps{
		Writer: writer, Importer: curl.NewImporter(), CertificateManager: manager,
		Config: config.Default(configDir), ConfigDir: configDir,
	})
	m.importColID = "collection-1"
	m.importPreview = &curl.ImportResult{
		Method: http.MethodPost,
		URL:    "https://example.com/token",
		Headers: http.Header{
			"Content-Type": {"application/x-www-form-urlencoded"},
			"X-Tag":        {"one", "two"},
		},
		Body: "user_id=abc&grant_type=Cert",
		Certificate: &curl.CertificateSpec{
			File: filepath.Join(configDir, "client.p12"), Type: "P12", Password: "literal-password",
		},
	}
	m.mode = importMode

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	require.Equal(t, normalMode, m.mode)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, writer.request)
	require.JSONEq(t, `{"Content-Type":["application/x-www-form-urlencoded"],"X-Tag":["one","two"]}`, writer.request.Headers)
	require.Equal(t, "user_id=abc&grant_type=Cert", writer.request.Body)
	require.Len(t, manager.cfg.HTTP.ClientCertificates, 1)
	cert := manager.cfg.HTTP.ClientCertificates[0]
	require.Equal(t, "example.com", cert.Host)
	require.Equal(t, "literal-password", cert.Password)

	raw, err := config.Load(configDir)
	require.NoError(t, err)
	require.Equal(t, "literal-password", raw.HTTP.ClientCertificates[0].Password)
}

func TestImportCancelDoesNotChangeCurrentURL(t *testing.T) {
	m := New(Deps{Importer: curl.NewImporter()})
	m.urlInput.SetValue("https://current.example")
	m = m.openCurlImport()
	m.importInput.SetValue(strings.ReplaceAll(exactIAMCurl, "access.dev", "other.dev"))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	require.Equal(t, normalMode, m.mode)
	require.Equal(t, "https://current.example", m.urlInput.Value())
}
