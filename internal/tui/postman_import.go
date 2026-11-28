package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/postmanimport"
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
		result, err := importer.ImportPostman(ctx, path)
		return postmanImportResultMsg{result: result, err: err}
	}
}

// formatPostmanImportResult renders an import result for the modal, which
// shows a handful of rows: totals first, then one line per collection, then
// environments and their errors.
func formatPostmanImportResult(result postmanimport.Result, err error) string {
	if err != nil {
		return "Import failed: " + err.Error()
	}
	summary := result.Summary()
	var lines []string
	if len(result.Collections) > 1 {
		lines = append(lines, fmt.Sprintf(
			"%d requests imported across %d collections · %d warnings · %d errors",
			summary.RequestsImported, len(result.Collections), summary.Warnings, summary.Errors,
		))
	}
	for _, collection := range result.Collections {
		lines = append(lines, formatPostmanCollectionLine(collection))
	}
	environments := result.Environments
	if environments.Imported > 0 || len(environments.Errors) > 0 {
		line := fmt.Sprintf("Environments: %d imported", environments.Imported)
		if len(environments.Errors) > 0 {
			line += fmt.Sprintf(", %d errors", len(environments.Errors))
		}
		lines = append(lines, line)
		lines = append(lines, environments.Errors...)
	}
	if len(lines) == 0 {
		return "Nothing was imported."
	}
	return strings.Join(lines, "\n")
}

func formatPostmanCollectionLine(collection postmanimport.CollectionResult) string {
	if collection.Err != nil {
		return fmt.Sprintf("Failed %s: %v", filepath.Base(collection.FilePath), collection.Err)
	}
	if collection.Outcome == postmanimport.OutcomeSkipped {
		return fmt.Sprintf("Skipped %q (%d requests)", collection.Name, collection.Total)
	}
	line := fmt.Sprintf(
		"Imported %d/%d requests into %q",
		collection.Imported, collection.Total, collection.Name,
	)
	if collection.Outcome != postmanimport.OutcomeCreated && collection.Outcome != "" {
		line += " (" + string(collection.Outcome) + ")"
	}
	if len(collection.Warnings) > 0 {
		line += fmt.Sprintf(" · %d warnings", len(collection.Warnings))
	}
	if collection.Security > postman.Safe {
		line += " · may contain credentials"
	}
	return line
}
