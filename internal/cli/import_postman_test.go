package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/postmanimport"
	"github.com/crazy-vedic/quark/internal/store"
)

// The import orchestration (duplicates, merge, nested folders, variables,
// environments, bulk exports) is tested in internal/postmanimport. The tests
// here cover the command surface: flags, prompts and the printed report.

const apiCollectionJSON = `{"info":{"name":"API","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},` +
	`"item":[{"name":"Incoming","request":{"method":"GET","url":"https://example.test/incoming"}}]}`

func TestImportPostmanCommandSingleFileAndErrors(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "collection.json")
	collection := `{"info":{"name":"Imported","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},` +
		`"item":[{"name":"Health","request":{"method":"GET","url":"https://example.test/health"}}]}`
	require.NoError(t, os.WriteFile(file, []byte(collection), 0600))
	cmd := NewImportPostmanCmd(st, NewDebugLogger(nil))
	var out, diagnostics bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&diagnostics)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{file, "--collection-name", "Override"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), `Imported 1/1 requests into collection "Override"`)
	collections, err := st.ListCollections(ctx)
	require.NoError(t, err)
	require.Len(t, collections, 1)
	assert.Equal(t, "Override", collections[0].Name)

	missing := NewImportPostmanCmd(st, NewDebugLogger(nil))
	missing.SetArgs([]string{filepath.Join(t.TempDir(), "missing.json")})
	assert.ErrorContains(t, missing.Execute(), "import-postman:")
	invalidAction := NewImportPostmanCmd(st, NewDebugLogger(nil))
	invalidAction.SetArgs([]string{file, "--on-duplicate", "invalid"})
	assert.ErrorContains(t, invalidAction.Execute(), "invalid --on-duplicate")
	invalidFile := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(invalidFile, []byte("not json"), 0600))
	parseFailure := NewImportPostmanCmd(st, NewDebugLogger(nil))
	parseFailure.SetArgs([]string{invalidFile})
	assert.ErrorContains(t, parseFailure.Execute(), "parse:")
}

func TestImportPostmanInteractiveDuplicatePrompt(t *testing.T) {
	oldProbe := terminalProbe
	terminalProbe = func(io.Reader) bool { return true }
	t.Cleanup(func() { terminalProbe = oldProbe })

	file := filepath.Join(t.TempDir(), "collection.json")
	require.NoError(t, os.WriteFile(file, []byte(apiCollectionJSON), 0600))

	for _, tc := range []struct {
		name      string
		input     string
		wantNames []string
		wantLabel string
	}{
		{"invalid then skip", "?\nskip\n", []string{"API"}, `Skipped "API"`},
		{"replace", "replace\n", []string{"API"}, "(replaced)"},
		{"duplicate", "duplicate\n", []string{"API", "API 1"}, "(duplicated)"},
		{"merge", "merge\n", []string{"API"}, "(merged)"},
		{"empty line keeps asking", "\nduplicate\n", []string{"API", "API 1"}, "(duplicated)"},
		{"apply to all", "apply\nmerge\n", []string{"API"}, "(merged)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, st.Close()) })
			ctx := context.Background()
			require.NoError(t, st.SaveCollection(ctx, &domain.Collection{Name: "API"}))

			cmd := NewImportPostmanCmd(st, NewDebugLogger(nil))
			cmd.SetIn(strings.NewReader(tc.input))
			var out, diagnostics bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&diagnostics)
			cmd.SetArgs([]string{file})
			require.NoError(t, cmd.Execute())

			assert.Contains(t, out.String(), `Collection "API" already exists`)
			assert.Contains(t, out.String(), tc.wantLabel)
			cols, err := st.ListCollections(ctx)
			require.NoError(t, err)
			names := make([]string, 0, len(cols))
			for _, c := range cols {
				names = append(names, c.Name)
			}
			assert.Equal(t, tc.wantNames, names)
		})
	}
}

func TestPrintImportResultReportsOutcomes(t *testing.T) {
	cmd := &cobra.Command{}
	var out, diagnostics bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&diagnostics)
	printImportResult(cmd, postmanimport.CollectionResult{
		Name:    "Skipped",
		Total:   3,
		Outcome: postmanimport.OutcomeSkipped,
	})
	assert.Contains(t, out.String(), `Skipped "Skipped" (3 requests)`)

	out.Reset()
	printImportResult(cmd, postmanimport.CollectionResult{
		Name:     "Review",
		Imported: 1,
		Total:    2,
		Warnings: []string{"warning"},
		Security: postman.Dangerous,
		Outcome:  postmanimport.OutcomeMerged,
	})
	assert.Contains(t, out.String(), `(merged) (1 warnings)`)
	assert.Contains(t, diagnostics.String(), "Security: Dangerous")
	assert.Contains(t, diagnostics.String(), "Warning: warning")

	out.Reset()
	printImportResult(cmd, postmanimport.CollectionResult{Err: os.ErrNotExist})
	assert.Empty(t, out.String())
}

func TestIsTerminalUsesProbe(t *testing.T) {
	previous := terminalProbe
	terminalProbe = func(io.Reader) bool { return true }
	t.Cleanup(func() { terminalProbe = previous })
	require.True(t, isTerminal(bytes.NewBufferString("input")))
}
