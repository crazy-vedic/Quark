//go:build e2e

package cli

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// A JSON body that is not valid JSON is refused before anything is sent.
func TestCLI_RunRefusesInvalidJSONBody(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv, got := newCapturingServer(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Broken", Method: http.MethodPost, URL: srv.URL + "/x",
		Headers: `{"Content-Type":["application/json"]}`,
		Body:    `{"id": 1,}`,
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Broken")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out+errOut, "invalid JSON body")
	got.mu.Lock()
	defer got.mu.Unlock()
	assert.Empty(t, got.body, "nothing reaches the server")
}

// Variable values are inserted as written, so a value that is itself a
// placeholder leaves a template in the final body. It is sent as written
// instead of being refused as invalid JSON.
func TestCLI_RunSendsBodyWhoseValueIsAPlaceholder(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv, got := newCapturingServer(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Template", Method: http.MethodPost, URL: srv.URL + "/x",
		Headers: `{"Content-Type":["application/json"]}`,
		Body:    `{"id": {{id}}}`,
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Template", "--var", "id={{other}}")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	got.mu.Lock()
	defer got.mu.Unlock()
	assert.Equal(t, `{"id": {{other}}}`, got.body)
}
