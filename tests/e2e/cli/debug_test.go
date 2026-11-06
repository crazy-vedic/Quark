//go:build e2e

package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestE2E_DebugModeInitializesLogsForCommands(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runQuarkWithHome(
		t,
		home,
		"--debug",
		"--config",
		filepath.Join(home, ".quark"),
		"collection",
		"list",
	)
	require.Equal(t, 0, code, stderr)
}
