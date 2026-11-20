//go:build e2e

package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/store"
)

// Saving a second request under a name the collection already has fails with a
// message that says what is wrong, not a raw database error.
func TestCLI_ImportCurlWithDuplicateNameExplainsTheRefusal(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	const command = "curl https://example.test/one"

	out, errOut, code := runQuarkWithHome(t, home, "import", "curl", command, "--collection", colID, "--name", "Same")
	require.Equal(t, 0, code, "first import failed: %s %s", out, errOut)

	out, errOut, code = runQuarkWithHome(t, home, "import", "curl", command, "--collection", colID, "--name", "Same")
	assert.NotEqual(t, 0, code, "a duplicate name must be refused")
	combined := out + errOut
	assert.Contains(t, combined, `a request named "Same" already exists in this collection`)
	assert.NotContains(t, combined, "UNIQUE constraint failed", "the raw database error must not leak")

	st, err := store.New(filepath.Join(home, ".quark", "quark.db"))
	require.NoError(t, err)
	defer st.Close()
	reqs, err := st.ListRequests(context.Background(), colID)
	require.NoError(t, err)
	assert.Len(t, reqs, 1, "the duplicate must not be saved")
}
