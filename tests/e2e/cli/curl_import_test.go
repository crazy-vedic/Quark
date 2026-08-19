//go:build e2e

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/store"
)

func TestCLI_CurlImportPersistsAndExecutesRequest(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	command := `curl -X POST 'https://example.test/token' --header 'Content-Type: application/x-www-form-urlencoded' --header 'X-Trace: first' --header 'X-Trace: second' --data-urlencode 'user_id=abc==' --data-urlencode 'scope=offline_access openid'`

	out, errOut, code := runQuarkWithHome(t, home, "import", "curl", command, "--collection", colID, "--name", "IAM token")
	require.Equal(t, 0, code, "import failed: %s", errOut)
	require.NotContains(t, out, "offline_access")

	st, err := store.New(filepath.Join(home, ".quark", "quark.db"))
	require.NoError(t, err)
	defer st.Close()
	requests, err := st.ListRequests(context.Background(), colID)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Equal(t, "IAM token", requests[0].Name)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "user_id=abc%3D%3D&scope=offline_access%20openid", requests[0].Body)
	var headers http.Header
	require.NoError(t, json.Unmarshal([]byte(requests[0].Headers), &headers))
	require.Equal(t, []string{"first", "second"}, headers.Values("X-Trace"))

	// The request is executable by the production binary after import.
	out, errOut, code = runQuarkWithHome(t, home, "run", "API/IAM token")
	require.NotEqual(t, 0, code, "example.test should not be reachable; command must still resolve the imported request: %s", errOut)
	require.NotContains(t, out+errOut, "request not found")
}
