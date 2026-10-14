package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
)

type certificateManagerStub struct {
	reloads []config.Config
	err     error
}

func updatedCertificateModel(t *testing.T, updated tea.Model) Model {
	t.Helper()
	model, ok := updated.(Model)
	require.True(t, ok, "updated model should remain a TUI Model")
	return model
}

func requireModel(t *testing.T, model tea.Model) Model {
	t.Helper()
	got, ok := model.(Model)
	if !ok {
		t.Fatalf("expected Model, got %T", model)
	}
	return got
}

func (s *certificateManagerStub) Reload(cfg config.Config) error {
	s.reloads = append(s.reloads, cfg)
	return s.err
}

func certEditModel(dir string, manager *certificateManagerStub) Model {
	cfg := config.Default(dir)
	m := New(Deps{Config: cfg, ConfigDir: dir, CertificateManager: manager})
	m = m.openClientCertModal().beginClientCertEdit(-1)
	return m
}

func setCertFields(m Model, host, typ, file, ca string) Model {
	m.clientCertHost.SetValue(host)
	m.clientCertType.SetValue(typ)
	m.clientCertFile.SetValue(file)
	m.clientCertCAFile.SetValue(ca)
	return m
}

func TestClientCertificateModalNavigationAndEditing(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())}).openClientCertModal()
	_, _ = m.handleClientCertKey(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ := m.handleClientCertKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updatedCertificateModel(t, updated)
	require.True(t, m.clientCertEditing)
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyTab}, {Type: tea.KeyTab}, {Type: tea.KeyTab},
		{Type: tea.KeyTab}, {Type: tea.KeyTab}, {Type: tea.KeyTab},
		{Type: tea.KeyShiftTab}, {Type: tea.KeyRunes, Runes: []rune{'x'}},
	} {
		updated, _ = m.Update(key)
		m = updatedCertificateModel(t, updated)
	}
	require.NotEmpty(t, m.clientCertPassword.Value())
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updatedCertificateModel(t, updated)
	require.False(t, m.clientCertEditing)
	updated, _ = m.handleClientCertKey(tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, normalMode, updatedCertificateModel(t, updated).mode)
}

func TestClientCertificateSaveValidationDefaultsAndReplace(t *testing.T) {
	dir := t.TempDir()
	manager := &certificateManagerStub{}
	m := certEditModel(dir, manager)
	updated, _ := m.saveClientCert()
	m = updatedCertificateModel(t, updated)
	require.Contains(t, m.clientCertError, "Host and a certificate")

	m = setCertFields(m, " API.Example.COM ", "", "client.P12", "")
	updated, _ = m.saveClientCert()
	m = updatedCertificateModel(t, updated)
	require.Equal(t, "api.example.com", m.clientCerts[0].Host)
	require.Equal(t, "P12", m.clientCerts[0].Type)

	m = m.beginClientCertEdit(0)
	m = setCertFields(m, "api.example.com", "bogus", "client.pem", "")
	updated, _ = m.saveClientCert()
	require.Contains(t, updatedCertificateModel(t, updated).clientCertError, "must be P12 or PEM")

	m = m.beginClientCertEdit(0)
	m = setCertFields(m, " API.EXAMPLE.COM ", "pem", "client.pem", "")
	updated, _ = m.saveClientCert()
	m = updatedCertificateModel(t, updated)
	require.Len(t, m.clientCerts, 1)
	require.Equal(t, "PEM", m.clientCerts[0].Type)
	require.Empty(t, m.clientCertError)
}

func TestClientCertificateSaveAndDeleteFailureRecovery(t *testing.T) {
	dir := t.TempDir()
	manager := &certificateManagerStub{err: errors.New("bad certificate")}
	m := setCertFields(certEditModel(dir, manager), "api.example.com", "pem", "client.pem", "")
	updated, _ := m.saveClientCert()
	m = updatedCertificateModel(t, updated)
	require.Contains(t, m.clientCertError, "bad certificate")
	require.Empty(t, m.clientCerts)

	manager.err = nil
	m = setCertFields(m, "api.example.com", "pem", "client.pem", "")
	updated, _ = m.saveClientCert()
	m = updatedCertificateModel(t, updated)
	require.Len(t, m.clientCerts, 1)

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	m.configDir = filepath.Join(blocker, "child")
	m.clientCertCursor = 0
	updated, _ = m.deleteClientCert()
	m = updatedCertificateModel(t, updated)
	require.Contains(t, m.clientCertError, "Save failed")
	require.Len(t, m.clientCerts, 1)
	require.GreaterOrEqual(t, len(manager.reloads), 3)
}

func TestClientCertificateDeleteAndViewBoundaries(t *testing.T) {
	m := New(Deps{Config: config.Default(t.TempDir())}).openClientCertModal()
	_, _ = m.deleteClientCert()
	require.Contains(t, m.viewClientCertModal(), "No certificates configured")

	m.clientCerts = []config.ClientCertificate{
		{Host: "a.example", Type: "PEM", CAFile: "ca.pem"},
		{Host: "b.example", Type: "P12", File: "b.p12"},
	}
	m.clientCertCursor = 1
	require.Contains(t, m.viewClientCertModal(), "b.p12")
	m.clientCertEditing = true
	m.clientCertError = "invalid"
	require.Contains(t, m.viewClientCertModal(), "Password:")
	require.Contains(t, m.viewClientCertModal(), "invalid")
}

func TestClientCertificateGlobalKeyOpensModal(t *testing.T) {
	cfg := config.Default(t.TempDir())
	m := New(Deps{Config: cfg, Ctx: context.Background()})
	m.width, m.height = 100, 30

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	got, ok := updated.(Model)
	require.True(t, ok)
	require.Equal(t, clientCertMode, got.mode)
}

func TestClientCertificateSavePersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	manager := &certificateManagerStub{}
	cfg := config.Default(dir)
	m := New(Deps{Config: cfg, ConfigDir: dir, CertificateManager: manager})
	m = m.openClientCertModal()
	m = m.beginClientCertEdit(-1)
	m.clientCertHost.SetValue("access.dev.example.com")
	m.clientCertFile.SetValue(`C:\certs\dev.p12`)
	m.clientCertPassword.SetValue("secret")

	updated, _ := m.saveClientCert()
	got, ok := updated.(Model)
	require.True(t, ok)
	require.Len(t, got.cfg.HTTP.ClientCertificates, 1)
	require.Equal(t, "access.dev.example.com", got.cfg.HTTP.ClientCertificates[0].Host)
	require.Len(t, manager.reloads, 1)

	loaded, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, got.cfg.HTTP.ClientCertificates, loaded.HTTP.ClientCertificates)
}

func TestClientCertificateConfiguredBindingsDriveModal(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	cfg.Keybindings.ClientCerts = "z"
	cfg.Keybindings.CertDown = "n"
	cfg.Keybindings.CertAdd = "x"
	cfg.Keybindings.CertClose = "q"
	m := New(Deps{Config: cfg, ConfigDir: dir, Ctx: context.Background()})
	m.width, m.height = 100, 30

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	got := requireModel(t, updated)
	require.Equal(t, clientCertMode, got.mode)
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	got = requireModel(t, updated)
	require.True(t, got.clientCertEditing)
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	got = requireModel(t, updated)
	require.False(t, got.clientCertEditing)
	view := got.View()
	require.Contains(t, view, "[x] add")
	require.Contains(t, view, "[q] close")
}

func TestClientCertificateSaveValidatesAndInfersPEM(t *testing.T) {
	dir := t.TempDir()
	m := New(Deps{Config: config.Default(dir), ConfigDir: dir})
	m = m.openClientCertModal().beginClientCertEdit(-1)
	updated, _ := m.saveClientCert()
	got := requireModel(t, updated)
	require.Contains(t, got.clientCertError, "Host and a certificate")

	got.clientCertHost.SetValue(" API.Example.COM ")
	got.clientCertFile.SetValue("client.pem")
	updated, _ = got.saveClientCert()
	got = requireModel(t, updated)
	require.Equal(t, "api.example.com", got.cfg.HTTP.ClientCertificates[0].Host)
	require.Equal(t, "PEM", got.cfg.HTTP.ClientCertificates[0].Type)
	require.False(t, got.clientCertEditing)
}
