//go:build e2e

package cli

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// API-key auth "in query" adds one parameter. It must not reorder or re-encode
// the query the user wrote, and an existing parameter of that name is replaced.
func TestCLI_RunAPIKeyInQueryKeepsTheUsersQuery(t *testing.T) {
	var mu sync.Mutex
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rawQuery = r.URL.RawQuery
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			"appended after the existing query",
			"/p?zeta=1&alpha=a%20b",
			"zeta=1&alpha=a%20b&api_key=s3cret%26x",
		},
		{"added to a URL with no query", "/p", "api_key=s3cret%26x"},
		{"replaces a parameter of the same name", "/p?api_key=old&b=2", "api_key=s3cret%26x&b=2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			seedRequest(t, home, colID, &domain.Request{
				Name:       "Keyed",
				Method:     http.MethodGet,
				URL:        srv.URL + tc.path,
				AuthType:   domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"api_key","value":"s3cret&x"}`,
			})

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Keyed")
			require.Equal(t, 0, code, "run failed: %s %s", out, errOut)

			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, tc.want, rawQuery)
		})
	}
}
