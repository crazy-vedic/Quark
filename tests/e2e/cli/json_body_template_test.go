//go:build e2e

package cli

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

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
