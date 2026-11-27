//go:build e2e

package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func TestCLI_Run_EscapedOpenerAndDynamicVariables(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	quarkDir := filepath.Join(home, ".quark")
	dbPath := filepath.Join(quarkDir, "quark.db")
	require.NoError(t, os.MkdirAll(quarkDir, 0o700))

	var mu sync.Mutex
	var gotPath, gotBody, gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotPath, gotBody, gotID = r.URL.Path, string(body), r.Header.Get("X-Id")
		mu.Unlock()
		w.WriteHeader(204)
	}))
	t.Cleanup(srv.Close)

	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	col := &domain.Collection{ID: "col-escape", Name: "Escape"}
	req := &domain.Request{
		ID:           "req-escape",
		CollectionID: col.ID,
		Name:         "Send",
		Method:       "POST",
		URL:          srv.URL + `/lit/\{{x}}`,
		Headers:      `{"X-Id": "{{$guid}}"}`,
		Body:         `{"note": "\{{raw}}", "id": "{{$randomUUID}}"}`,
	}
	require.NoError(t, st.SaveCollection(ctx, col))
	require.NoError(t, st.SaveRequest(ctx, req))
	require.NoError(t, st.Close())

	out, stderr, code := runQuarkWithHome(t, home, "run", "Escape/Send")
	require.Equal(t, 0, code, "run must succeed without any environment: stdout=%s stderr=%s",
		out, stderr)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "/lit/{{x}}", gotPath, "escaped opener must reach the server literally")
	uuidRE := regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	assert.Regexp(t, uuidRE, gotID, "$guid must be substituted in headers")
	assert.Contains(t, gotBody, `"note": "{{raw}}"`)
	assert.Regexp(t, `"id": "[0-9a-f-]{36}"`, gotBody, "$randomUUID must be substituted in body")
}
