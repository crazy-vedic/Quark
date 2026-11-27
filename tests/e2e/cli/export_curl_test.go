//go:build e2e

package cli

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
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

// setEnvVars writes vars into the collection's default environment, makes it
// active, and writes globals into Global.
func setEnvVars(t *testing.T, home, colID string, vars, globals map[string]string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	env, err := st.CreateDefaultEnvironment(ctx, colID)
	require.NoError(t, err)
	env.SetVars(vars)
	require.NoError(t, st.SaveEnvironment(ctx, env))
	require.NoError(t, st.SetActiveEnvironment(ctx, colID, env.ID))

	global, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	global.SetVars(globals)
	require.NoError(t, st.SaveEnvironment(ctx, global))
}

// Variables are filled in from the active environment, then Global, like `run`.
func TestCLI_RequestExportCurlResolvesVariablesFromActiveEnvironment(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	setEnvVars(t, home, colID,
		map[string]string{"base_url": "https://api.example.test", "token": "env-token"},
		map[string]string{"tenant": "global-tenant", "token": "global-token"})
	seedRequest(t, home, colID, &domain.Request{
		Name: "Templated", Method: http.MethodPost, URL: "{{base_url}}/{{tenant}}/users",
		Headers: `{"X-Token":["{{token}}"]}`, Body: `{"owner":"{{tenant}}"}`,
	})

	out, errOut, code := runQuarkWithHome(t, home, "request", "export-curl", "API/Templated")
	require.Equal(t, 0, code, "export failed: %s %s", out, errOut)
	assert.Contains(t, out, "'https://api.example.test/global-tenant/users'")
	assert.Contains(t, out, "-H 'X-Token: env-token'", "the collection environment beats Global")
	assert.Contains(t, out, `{"owner":"global-tenant"}`)
	assert.NotContains(t, out, "{{")
}

// A variable nothing defines fails the export instead of printing a command
// that would send the literal placeholder.
func TestCLI_RequestExportCurlRefusesUndefinedVariable(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	setEnvVars(t, home, colID, map[string]string{"base_url": "https://api.example.test"}, nil)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Missing", Method: http.MethodGet, URL: "{{base_url}}/{{nope}}",
	})

	out, errOut, code := runQuarkWithHome(t, home, "request", "export-curl", "API/Missing")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out+errOut, "nope")
	assert.NotContains(t, out, "curl -X")
}

func TestCLI_RequestExportCurlRefusesBadInput(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "NotJSON", Method: http.MethodGet, URL: "https://example.test/a", Headers: `{broken`,
	})
	seedRequest(t, home, colID, &domain.Request{
		Name:    "WrongType",
		Method:  http.MethodGet,
		URL:     "https://example.test/b",
		Headers: `{"X-Count":5}`,
	})

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"headers are not JSON", []string{"API/NotJSON"}, "parse request headers"},
		{
			"a header value is not a string",
			[]string{"API/WrongType"},
			`header "X-Count": value must be a string or string array`,
		},
		{"the request does not exist", []string{"API/Missing"}, "resolve request"},
		{"no request given", nil, "accepts 1 arg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := runQuarkWithHome(
				t,
				home,
				append([]string{"request", "export-curl"}, tc.args...)...)
			assert.NotEqual(t, 0, code)
			assert.Contains(t, out+errOut, tc.want)
			assert.NotContains(t, out, "curl -X", "nothing is printed on failure")
		})
	}
}
