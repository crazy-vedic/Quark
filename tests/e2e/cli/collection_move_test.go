//go:build e2e

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_CollectionMoveNestsAndRefusesCycles(t *testing.T) {
	home, _ := setupHomeWithCollection(t)

	out, errOut, code := runQuarkWithHome(t, home, "collection", "create", "Billing")
	require.Equal(t, 0, code, "create failed: %s %s", out, errOut)

	out, errOut, code = runQuarkWithHome(t, home, "collection", "move", "API", "Billing")
	require.Equal(t, 0, code, "move failed: %s %s", out, errOut)
	assert.Contains(t, out, `Moved collection "API" under "Billing"`)

	out, errOut, code = runQuarkWithHome(t, home, "collection", "list")
	require.Equal(t, 0, code, "list failed: %s %s", out, errOut)
	assert.Contains(t, out, "Billing/API")

	_, errOut, code = runQuarkWithHome(t, home, "collection", "move", "Billing", "Billing/API")
	require.NotEqual(t, 0, code, "moving a collection into its own descendant must fail")
	assert.Contains(t, errOut, "into itself or one of its descendants")

	out, errOut, code = runQuarkWithHome(t, home, "collection", "move", "Billing/API", "/")
	require.Equal(t, 0, code, "move to root failed: %s %s", out, errOut)
	assert.Contains(t, out, "to the root")

	out, errOut, code = runQuarkWithHome(t, home, "collection", "list")
	require.Equal(t, 0, code, "list failed: %s %s", out, errOut)
	assert.NotContains(t, out, "Billing/API")
}
