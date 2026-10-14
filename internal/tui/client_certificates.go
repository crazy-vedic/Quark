package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/config"
)

const (
	clientCertHostField = iota
	clientCertTypeField
	clientCertFileField
	clientCertKeyFileField
	clientCertCAFileField
	clientCertPasswordField
	clientCertFieldCount
)

func (m Model) openClientCertModal() Model {
	m.mode = clientCertMode
	m.clientCerts = append([]config.ClientCertificate(nil), m.cfg.HTTP.ClientCertificates...)
	m.clientCertCursor = 0
	m.clientCertEditing = false
	m.clientCertError = ""
	return m
}

func (m Model) closeClientCertModal() Model {
	m.mode = normalMode
	m.clientCertEditing = false
	m.clientCertError = ""
	m.clientCertHost.Blur()
	m.clientCertType.Blur()
	m.clientCertFile.Blur()
	m.clientCertKeyFile.Blur()
	m.clientCertCAFile.Blur()
	m.clientCertPassword.Blur()
	return m
}

func (m Model) handleClientCertKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action, ok := m.resolver.Resolve(9, 0, msg)
	if !ok {
		if m.clientCertEditing {
			return m.updateClientCertInput(msg)
		}
		return m, nil
	}
	if m.clientCertEditing {
		switch action {
		case "close":
			return m.cancelClientCertEdit(), nil
		case "next_field":
			return m.moveClientCertField(1), nil
		case "prev_field":
			return m.moveClientCertField(-1), nil
		case "save":
			return m.saveClientCert()
		default:
			return m.updateClientCertInput(msg)
		}
	}
	switch action {
	case "close":
		return m.closeClientCertModal(), nil
	case "navigate_down":
		if m.clientCertCursor < len(m.clientCerts)-1 {
			m.clientCertCursor++
		}
	case "navigate_up":
		if m.clientCertCursor > 0 {
			m.clientCertCursor--
		}
	case "add":
		return m.beginClientCertEdit(-1), nil
	case "edit":
		if len(m.clientCerts) > 0 {
			return m.beginClientCertEdit(m.clientCertCursor), nil
		}
	case "delete":
		return m.deleteClientCert()
	}
	return m, nil
}

func (m Model) beginClientCertEdit(index int) Model {
	m.clientCertEditing = true
	m.clientCertField = clientCertHostField
	m.clientCertError = ""
	m.clientCertHost = textinput.New()
	m.clientCertHost.Placeholder = "api.example.com"
	m.clientCertHost.CharLimit = 253
	m.clientCertType = textinput.New()
	m.clientCertType.Placeholder = "P12 or PEM"
	m.clientCertType.CharLimit = 8
	m.clientCertFile = textinput.New()
	m.clientCertFile.Placeholder = "C:\\path\\client.p12 or client.pem"
	m.clientCertFile.CharLimit = 2048
	m.clientCertKeyFile = textinput.New()
	m.clientCertKeyFile.Placeholder = "C:\\path\\client-key.pem (PEM)"
	m.clientCertKeyFile.CharLimit = 2048
	m.clientCertCAFile = textinput.New()
	m.clientCertCAFile.Placeholder = "C:\\path\\ca.pem (optional)"
	m.clientCertCAFile.CharLimit = 2048
	m.clientCertPassword = textinput.New()
	m.clientCertPassword.EchoMode = textinput.EchoPassword
	m.clientCertPassword.CharLimit = 512
	m.styleInputs()
	if index >= 0 && index < len(m.clientCerts) {
		cert := m.clientCerts[index]
		m.clientCertHost.SetValue(cert.Host)
		m.clientCertType.SetValue(cert.Type)
		m.clientCertFile.SetValue(cert.File)
		m.clientCertKeyFile.SetValue(cert.KeyFile)
		m.clientCertCAFile.SetValue(cert.CAFile)
		m.clientCertPassword.SetValue(cert.Password)
	}
	m.clientCertHost.Focus()
	return m
}

func (m Model) cancelClientCertEdit() Model {
	m.clientCertEditing = false
	m.clientCertError = ""
	m.clientCertHost.Blur()
	m.clientCertType.Blur()
	m.clientCertFile.Blur()
	m.clientCertKeyFile.Blur()
	m.clientCertCAFile.Blur()
	m.clientCertPassword.Blur()
	return m
}

func (m Model) moveClientCertField(step int) Model {
	m.clientCertField = (m.clientCertField + step + clientCertFieldCount) % clientCertFieldCount
	m.focusClientCertField()
	return m
}

func (m Model) updateClientCertInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.clientCertField {
	case clientCertHostField:
		m.clientCertHost, cmd = m.clientCertHost.Update(msg)
	case clientCertTypeField:
		m.clientCertType, cmd = m.clientCertType.Update(msg)
	case clientCertFileField:
		m.clientCertFile, cmd = m.clientCertFile.Update(msg)
	case clientCertKeyFileField:
		m.clientCertKeyFile, cmd = m.clientCertKeyFile.Update(msg)
	case clientCertCAFileField:
		m.clientCertCAFile, cmd = m.clientCertCAFile.Update(msg)
	case clientCertPasswordField:
		m.clientCertPassword, cmd = m.clientCertPassword.Update(msg)
	}
	return m, cmd
}

func (m *Model) focusClientCertField() {
	m.clientCertHost.Blur()
	m.clientCertType.Blur()
	m.clientCertFile.Blur()
	m.clientCertKeyFile.Blur()
	m.clientCertCAFile.Blur()
	m.clientCertPassword.Blur()
	switch m.clientCertField {
	case clientCertHostField:
		m.clientCertHost.Focus()
	case clientCertTypeField:
		m.clientCertType.Focus()
	case clientCertFileField:
		m.clientCertFile.Focus()
	case clientCertKeyFileField:
		m.clientCertKeyFile.Focus()
	case clientCertCAFileField:
		m.clientCertCAFile.Focus()
	case clientCertPasswordField:
		m.clientCertPassword.Focus()
	}
}

func (m Model) saveClientCert() (tea.Model, tea.Cmd) {
	host := strings.ToLower(strings.TrimSpace(m.clientCertHost.Value()))
	file := strings.TrimSpace(m.clientCertFile.Value())
	certType := strings.ToUpper(strings.TrimSpace(m.clientCertType.Value()))
	keyFile := strings.TrimSpace(m.clientCertKeyFile.Value())
	caFile := strings.TrimSpace(m.clientCertCAFile.Value())
	password := m.clientCertPassword.Value()
	if host == "" || (file == "" && caFile == "") {
		m.clientCertError = "Host and a certificate or CA file are required"
		return m, nil
	}
	if certType == "" && file != "" {
		lower := strings.ToLower(file)
		if strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
			certType = "P12"
		} else {
			certType = "PEM"
		}
	}
	if certType != "" && certType != "P12" && certType != "PEM" {
		m.clientCertError = "Certificate type must be P12 or PEM"
		return m, nil
	}
	next := append([]config.ClientCertificate(nil), m.clientCerts...)
	entry := config.ClientCertificate{
		Host:     host,
		File:     file,
		Type:     certType,
		KeyFile:  keyFile,
		CAFile:   caFile,
		Password: password,
	}
	updated := false
	for i := range next {
		if strings.EqualFold(next[i].Host, host) {
			next[i] = entry
			m.clientCertCursor = i
			updated = true
			break
		}
	}
	if !updated {
		next = append(next, entry)
		m.clientCertCursor = len(next) - 1
	}
	nextCfg := m.cfg
	nextCfg.HTTP.ClientCertificates = next
	if m.certificateManager != nil {
		if err := m.certificateManager.Reload(nextCfg); err != nil {
			m.clientCertError = fmt.Sprintf("Certificate failed: %v", err)
			return m, nil
		}
	}
	if err := config.SaveClientCertificates(m.configDir, next); err != nil {
		if m.certificateManager != nil {
			_ = m.certificateManager.Reload(m.cfg)
		}
		m.clientCertError = fmt.Sprintf("Save failed: %v", err)
		return m, nil
	}
	m.cfg = nextCfg
	m.clientCerts = next
	m.clientCertEditing = false
	m.clientCertError = ""
	m.clientCertHost.Blur()
	m.clientCertType.Blur()
	m.clientCertFile.Blur()
	m.clientCertKeyFile.Blur()
	m.clientCertCAFile.Blur()
	m.clientCertPassword.Blur()
	m.statusSuccess = "Client certificate saved"
	return m, nil
}

func (m Model) deleteClientCert() (tea.Model, tea.Cmd) {
	if len(m.clientCerts) == 0 || m.clientCertCursor >= len(m.clientCerts) {
		return m, nil
	}
	next := append([]config.ClientCertificate(nil), m.clientCerts[:m.clientCertCursor]...)
	next = append(next, m.clientCerts[m.clientCertCursor+1:]...)
	nextCfg := m.cfg
	nextCfg.HTTP.ClientCertificates = next
	if m.certificateManager != nil {
		if err := m.certificateManager.Reload(nextCfg); err != nil {
			m.clientCertError = fmt.Sprintf("Certificate reload failed: %v", err)
			return m, nil
		}
	}
	if err := config.SaveClientCertificates(m.configDir, next); err != nil {
		if m.certificateManager != nil {
			_ = m.certificateManager.Reload(m.cfg)
		}
		m.clientCertError = fmt.Sprintf("Save failed: %v", err)
		return m, nil
	}
	m.cfg = nextCfg
	m.clientCerts = next
	if m.clientCertCursor >= len(next) && m.clientCertCursor > 0 {
		m.clientCertCursor--
	}
	m.statusSuccess = "Client certificate deleted"
	return m, nil
}

func (m Model) viewClientCertModal() string {
	var sb strings.Builder
	sb.WriteString(m.styles.titleStyle.Render("Client Certificates (mTLS)") + "\n\n")
	sb.WriteString(
		m.styles.mutedStyle.Render("Certificates are selected by request hostname.") + "\n\n",
	)
	if len(m.clientCerts) == 0 {
		sb.WriteString(
			m.styles.mutedStyle.Render("No certificates configured. Press a to add one.") + "\n",
		)
	} else {
		for i, cert := range m.clientCerts {
			cursor := "  "
			if i == m.clientCertCursor {
				cursor = "▸ "
			}
			material := cert.File
			if material == "" {
				material = cert.CAFile
			}
			line := fmt.Sprintf("%s%-35s %-4s %s", cursor, cert.Host, cert.Type, material)
			if i == m.clientCertCursor {
				sb.WriteString(lipgloss.NewStyle().Bold(true).Render(line) + "\n")
			} else {
				sb.WriteString(m.styles.mutedStyle.Render(line) + "\n")
			}
		}
	}
	if m.clientCertEditing {
		sb.WriteString("\n" + m.styles.titleStyle.Render("Add / Edit Certificate") + "\n")
		sb.WriteString("Host:     " + m.clientCertHost.View() + "\n")
		sb.WriteString("Type:     " + m.clientCertType.View() + "\n")
		sb.WriteString("Cert:     " + m.clientCertFile.View() + "\n")
		sb.WriteString("Key:      " + m.clientCertKeyFile.View() + "\n")
		sb.WriteString("CA:       " + m.clientCertCAFile.View() + "\n")
		sb.WriteString("Password: " + m.clientCertPassword.View() + "\n")
		sb.WriteString(m.styles.mutedStyle.Render(m.renderHints([]hintItem{
			{Label: "next field", Actions: []string{"cert_next_field"}},
			{Label: "previous field", Actions: []string{"cert_prev_field"}},
			{Label: "save", Actions: []string{"cert_save"}},
			{Label: "cancel", Actions: []string{"cert_close"}},
		})) + "\n")
	}
	if m.clientCertError != "" {
		sb.WriteString("\n" + m.styles.errorStyle.Render("✗ "+m.clientCertError) + "\n")
	}
	sb.WriteString(
		"\n" + m.styles.mutedStyle.Render(m.renderHints([]hintItem{
			{Label: "add", Actions: []string{"cert_add"}},
			{Label: "edit", Actions: []string{"cert_edit"}, IncludeAliases: true},
			{Label: "delete", Actions: []string{"cert_delete"}},
			{Label: "move", Actions: []string{"cert_up", "cert_down"}, IncludeAliases: true},
			{Label: "close", Actions: []string{"cert_close"}},
		})),
	)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.accent).
		Padding(1, 2).
		Width(max(1, min(m.width-4, 110)))
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		box.Render(sb.String()),
	)
}
