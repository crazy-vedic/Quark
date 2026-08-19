package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
)

type certificateManagerStub struct {
	reloads []config.Config
}

func (s *certificateManagerStub) Reload(cfg config.Config) error {
	s.reloads = append(s.reloads, cfg)
	return nil
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
