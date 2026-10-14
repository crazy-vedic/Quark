package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) openPostmanImportModal() Model {
	m.mode = postmanImportMode
	m.postmanPath.SetValue("")
	m.postmanPath.Focus()
	m.postmanBusy = false
	m.postmanOutput = ""
	return m
}

func (m Model) closePostmanImportModal() Model {
	m.mode = normalMode
	m.postmanPath.Blur()
	m.postmanBusy = false
	return m
}

func (m Model) handlePostmanImportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.postmanBusy {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.closePostmanImportModal(), nil
	case tea.KeyEnter:
		path := strings.TrimSpace(m.postmanPath.Value())
		if path == "" {
			m.postmanOutput = "Enter the path to a Postman collection file or export directory."
			return m, nil
		}
		if m.postmanImporter == nil {
			m.postmanOutput = "Postman importer is unavailable."
			return m, nil
		}
		m.postmanBusy = true
		m.postmanOutput = "Importing..."
		return m, postmanImportCmd(m.ctx, m.postmanImporter, path)
	}
	var cmd tea.Cmd
	m.postmanPath, cmd = m.postmanPath.Update(msg)
	return m, cmd
}

func postmanImportCmd(ctx context.Context, importer PostmanImporter, path string) tea.Cmd {
	return func() tea.Msg {
		output, err := importer.ImportPostman(ctx, path)
		return postmanImportResultMsg{output: output, err: err}
	}
}
