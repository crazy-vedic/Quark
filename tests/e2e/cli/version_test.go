//go:build e2e

package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCLI_VersionFlags(t *testing.T) {
	shortOut, shortErr, shortCode := runQuark(t, "-v")
	longOut, longErr, longCode := runQuark(t, "--version")

	require.Equal(t, 0, shortCode, "-v stderr: %s", shortErr)
	require.Equal(t, 0, longCode, "--version stderr: %s", longErr)
	require.Empty(t, shortErr)
	require.Empty(t, longErr)
	require.Equal(t, shortOut, longOut, "short and long version flags must report the same build")
	require.True(t, strings.HasPrefix(shortOut, "quark version "))
	require.Contains(t, shortOut, "commit ")
	require.Contains(t, shortOut, "built ")
}
