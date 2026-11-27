package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/postmanimport"
)

type postmanImporterStub struct {
	path   string
	result postmanimport.Result
	err    error
}

func (s *postmanImporterStub) ImportPostman(
	_ context.Context,
	path string,
) (postmanimport.Result, error) {
	s.path = path
	return s.result, s.err
}

func TestFormatPostmanImportResult(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		got := formatPostmanImportResult(postmanimport.Result{}, errors.New("bad dir"))
		require.Equal(t, "Import failed: bad dir", got)
	})
	t.Run("nothing", func(t *testing.T) {
		require.Equal(
			t,
			"Nothing was imported.",
			formatPostmanImportResult(postmanimport.Result{}, nil),
		)
	})
	t.Run("bulk summary and outcomes", func(t *testing.T) {
		result := postmanimport.Result{
			Bulk: true,
			Collections: []postmanimport.CollectionResult{
				{Name: "API", Outcome: postmanimport.OutcomeDuplicated, Imported: 2, Total: 2,
					Warnings: []string{"w"}, Security: postman.Dangerous},
				{Name: "Old", Outcome: postmanimport.OutcomeSkipped, Total: 4},
				{FilePath: "/exports/broken.json", Err: errors.New("parse: bad")},
			},
			Environments: postmanimport.EnvironmentResult{
				Imported: 1,
				Errors:   []string{"env.json: bad"},
			},
		}
		got := formatPostmanImportResult(result, nil)
		require.Contains(t, got, "2 requests imported across 3 collections · 1 warnings · 2 errors")
		require.Contains(
			t,
			got,
			`Imported 2/2 requests into "API" (duplicated) · 1 warnings · may contain credentials`,
		)
		require.Contains(t, got, `Skipped "Old" (4 requests)`)
		require.Contains(t, got, "Failed broken.json: parse: bad")
		require.Contains(t, got, "Environments: 1 imported, 1 errors")
		require.Contains(t, got, "env.json: bad")
	})
	t.Run("single file with merge", func(t *testing.T) {
		result := postmanimport.Result{Collections: []postmanimport.CollectionResult{
			{Name: "API", Outcome: postmanimport.OutcomeMerged, Imported: 1, Total: 3},
		}}
		require.Equal(
			t,
			`Imported 1/3 requests into "API" (merged)`,
			formatPostmanImportResult(result, nil),
		)
	})
}

func TestPostmanSidebarKeyOpensImportModalAndRunsImporter(t *testing.T) {
	importer := &postmanImporterStub{
		result: postmanimport.Result{Collections: []postmanimport.CollectionResult{
			{Name: "Example", Outcome: postmanimport.OutcomeCreated, Imported: 2, Total: 2},
		}},
	}
	m := New(Deps{PostmanImporter: importer})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = requireImportModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = requireImportModel(t, updated)
	require.Equal(t, postmanImportMode, m.mode)
	require.True(t, m.postmanPath.Focused())
	require.Contains(t, m.View(), "Import Postman collection")

	m.postmanPath.SetValue("  collection.json  ")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.True(t, m.postmanBusy)
	require.NotNil(t, cmd)

	updated, _ = m.Update(cmd())
	m = requireImportModel(t, updated)
	require.Equal(t, "collection.json", importer.path)
	require.False(t, m.postmanBusy)
	require.Contains(t, m.postmanOutput, `Imported 2/2 requests into "Example"`)
	require.Contains(t, m.View(), "Imported 2/2 requests")
}

func TestPostmanImportModalHandlesEmptyUnavailableFailureBusyAndCancel(t *testing.T) {
	m := New(Deps{}).openPostmanImportModal()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.Nil(t, cmd)
	require.Contains(t, m.postmanOutput, "Enter the path")

	m.postmanPath.SetValue("collection.json")
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.Nil(t, cmd)
	require.Contains(t, m.postmanOutput, "unavailable")

	importer := &postmanImporterStub{err: errors.New("invalid collection")}
	m.postmanImporter = importer
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.True(t, m.postmanBusy)
	require.NotNil(t, cmd)
	updated, ignored := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = requireImportModel(t, updated)
	require.Nil(t, ignored)
	require.True(t, m.postmanBusy)

	updated, _ = m.Update(cmd())
	m = requireImportModel(t, updated)
	require.False(t, m.postmanBusy)
	require.Equal(t, "Import failed: invalid collection", m.postmanOutput)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = requireImportModel(t, updated)
	require.Equal(t, normalMode, m.mode)
	require.False(t, m.postmanPath.Focused())
}

func TestPostmanKeyIsScopedToSidebarPane(t *testing.T) {
	m := New(Deps{}).WithFocus(requestPane)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = requireImportModel(t, updated)
	require.Equal(t, normalMode, m.mode)
}
