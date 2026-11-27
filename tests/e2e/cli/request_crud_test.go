//go:build e2e

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_RequestCreateShowRenameDelete(t *testing.T) {
	home, _ := setupHomeWithCollection(t)

	out, errOut, code := runQuarkWithHome(t, home, "request", "create", "API/Ping",
		"--method", "PUT", "--url", "https://api.example.test/ping")
	require.Equal(t, 0, code, "create failed: %s %s", out, errOut)
	assert.Contains(t, out, `Created request "API/Ping"`)

	out, errOut, code = runQuarkWithHome(t, home, "request", "show", "API/Ping")
	require.Equal(t, 0, code, "show failed: %s %s", out, errOut)
	assert.Contains(t, out, "PUT")
	assert.Contains(t, out, "https://api.example.test/ping")

	out, errOut, code = runQuarkWithHome(t, home, "request", "rename", "API/Ping", "Pong")
	require.Equal(t, 0, code, "rename failed: %s %s", out, errOut)

	_, errOut, code = runQuarkWithHome(t, home, "request", "delete", "API/Pong")
	require.NotEqual(t, 0, code, "delete without --yes must fail")
	assert.Contains(t, errOut, "without --yes")

	out, errOut, code = runQuarkWithHome(t, home, "request", "delete", "API/Pong", "--yes")
	require.Equal(t, 0, code, "delete failed: %s %s", out, errOut)

	out, errOut, code = runQuarkWithHome(t, home, "request", "list", "--collection", "API")
	require.Equal(t, 0, code, "list failed: %s %s", out, errOut)
	assert.Contains(t, out, "No requests found.")
}
