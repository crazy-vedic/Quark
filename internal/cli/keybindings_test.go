package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeybindingsListIsGrouped(t *testing.T) {
	configDir := t.TempDir()
	cmd := NewKeybindingsCmd(configDir)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list"})

	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), "[Global]")
	require.Contains(t, out.String(), "[Search]")
	require.Contains(t, out.String(), "search_select")
}

func TestKeybindingsSetPersistsAndRejectsConflict(t *testing.T) {
	configDir := t.TempDir()

	cmd := NewKeybindingsCmd(configDir)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"set", "search", "Z"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), "Set search = Z")

	raw, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, err)
	require.Contains(t, string(raw), "search = \"Z\"")

	conflict := NewKeybindingsCmd(configDir)
	conflict.SetArgs([]string{"set", "search", "q"})
	require.ErrorContains(t, conflict.Execute(), "conflict detected")
}

func TestKeybindingsResetRestoresDefaults(t *testing.T) {
	configDir := t.TempDir()
	set := NewKeybindingsCmd(configDir)
	set.SetArgs([]string{"set", "search", "Z"})
	require.NoError(t, set.Execute())

	reset := NewKeybindingsCmd(configDir)
	reset.SetArgs([]string{"reset"})
	require.NoError(t, reset.Execute())

	list := NewKeybindingsCmd(configDir)
	var out bytes.Buffer
	list.SetOut(&out)
	list.SetArgs([]string{"list"})
	require.NoError(t, list.Execute())
	require.Contains(t, out.String(), "search               /")
}
