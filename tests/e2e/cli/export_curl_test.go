//go:build e2e

package cli

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// Saved headers come in two shapes: a string, or an array of strings for a
// header sent more than once. Export must handle both and repeat -H per value.
func TestCLI_RequestExportCurlHandlesBothHeaderShapes(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:    "Mixed",
		Method:  http.MethodPost,
		URL:     "https://api.example.test/items",
		Headers: `{"X-Trace":["first","second"],"Accept":"application/json","X-One":["only"]}`,
	})

	out, errOut, code := runQuarkWithHome(t, home, "request", "export-curl", "API/Mixed")
	require.Equal(t, 0, code, "export failed: %s %s", out, errOut)

	assert.Contains(t, out, "curl -X 'POST'")
	assert.Contains(t, out, "-H 'Accept: application/json'")
	assert.Contains(t, out, "-H 'X-One: only'")
	assert.Contains(t, out, "-H 'X-Trace: first' -H 'X-Trace: second'",
		"each value of a repeated header is its own -H, in order")
	assert.Contains(t, out, "'https://api.example.test/items'")
}

func TestCLI_RequestExportCurlWithNoHeaders(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Bare", Method: http.MethodGet, URL: "https://api.example.test/bare",
	})

	out, errOut, code := runQuarkWithHome(t, home, "request", "export-curl", "API/Bare")
	require.Equal(t, 0, code, "export failed: %s %s", out, errOut)
	assert.Equal(t, "curl -X 'GET' 'https://api.example.test/bare'\n", out)
}

// The request is exported as stored, so variables are left for curl's user to fill in.
func TestCLI_RequestExportCurlLeavesVariablesUnresolved(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Templated", Method: http.MethodGet, URL: "{{base_url}}/users",
		Headers: `{"X-Token":["{{token}}"]}`,
	})

	out, errOut, code := runQuarkWithHome(t, home, "request", "export-curl", "API/Templated")
	require.Equal(t, 0, code, "export failed: %s %s", out, errOut)
	assert.Contains(t, out, "{{base_url}}/users")
	assert.Contains(t, out, "X-Token: {{token}}")
}

func TestCLI_RequestExportCurlRefusesBadInput(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "NotJSON", Method: http.MethodGet, URL: "https://example.test/a", Headers: `{broken`,
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "WrongType", Method: http.MethodGet, URL: "https://example.test/b", Headers: `{"X-Count":5}`,
	})

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"headers are not JSON", []string{"API/NotJSON"}, "parse request headers"},
		{"a header value is not a string", []string{"API/WrongType"}, `header "X-Count": value must be a string or string array`},
		{"the request does not exist", []string{"API/Missing"}, "resolve request"},
		{"no request given", nil, "accepts 1 arg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := runQuarkWithHome(t, home, append([]string{"request", "export-curl"}, tc.args...)...)
			assert.NotEqual(t, 0, code)
			assert.Contains(t, out+errOut, tc.want)
			assert.NotContains(t, out, "curl -X", "nothing is printed on failure")
		})
	}
}
