//go:build e2e

package cli

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// The CLI coverage profile includes shared packages used by the TUI as well.
// Exercise the shared export formatter and its configurable request shortcut so
// this profile reflects those changed lines instead of treating them as noise.
func TestCLI_SharedCurlExportAndShortcut(t *testing.T) {
	command, err := curl.ExportRequest(&domain.Request{
		Method:     "POST",
		URL:        "https://example.test/items?keep=yes",
		Headers:    `{"X-Trace":"one","Content-Type":"application/json"}`,
		AuthType:   domain.AuthTypeAPIKey,
		AuthConfig: `{"in":"query","name":"api_key","value":"secret value"}`,
		Body:       `{"item":"value"}`,
	})
	require.NoError(t, err)
	require.Contains(t, command, "-H 'Content-Type: application/json' -H 'X-Trace: one'")
	require.Contains(t, command, "'https://example.test/items?api_key=secret+value&keep=yes'")
	require.Contains(t, command, "--data-raw '{\"item\":\"value\"}'")

	binds := keybindings.DefaultKeybindings()
	resolver := keybindings.NewResolver(binds)
	got, ok := resolver.Resolve(0, 1, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(binds.ExportCurl)})
	require.True(t, ok)
	require.Equal(t, keybindings.ActionExportCurl, got)
	require.Empty(t, keybindings.Validate(binds))
}
