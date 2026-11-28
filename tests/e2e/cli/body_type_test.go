//go:build e2e

package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// bodyEchoServer records the Content-Type and body of the last request.
type bodyEchoServer struct {
	mu          sync.Mutex
	contentType string
	body        string
}

func newBodyEchoServer(t *testing.T) (*httptest.Server, *bodyEchoServer) {
	t.Helper()
	got := &bodyEchoServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		got.contentType = r.Header.Get("Content-Type")
		got.body = string(raw)
		got.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func (s *bodyEchoServer) last() (contentType, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contentType, s.body
}

// Each body type is sent as its own content: a form body is escaped after
// variables are filled in, JSON is sent as written, and none sends nothing.
func TestCLI_RunSendsBodyByType(t *testing.T) {
	cases := []struct {
		name        string
		bodyType    string
		body        string
		vars        map[string]string
		wantType    string
		wantPayload string
	}{
		{
			name:        "form values are escaped after variables are filled in",
			bodyType:    domain.BodyTypeFormURLEncoded,
			body:        "name={{name}}&lang=go",
			vars:        map[string]string{"name": "A&B C"},
			wantType:    "application/x-www-form-urlencoded",
			wantPayload: "name=A%26B+C&lang=go",
		},
		{
			name:        "json is sent as stored",
			bodyType:    domain.BodyTypeJSON,
			body:        `{"n": "{{name}}"}`,
			vars:        map[string]string{"name": "x"},
			wantType:    "application/json",
			wantPayload: `{"n": "x"}`,
		},
		{
			name:     "none sends no body",
			bodyType: domain.BodyTypeNone,
			body:     "ignored",
			wantType: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, got := newBodyEchoServer(t)
			home, colID := setupHomeWithCollection(t)
			setEnvVars(t, home, colID, tc.vars, nil)
			seedRequest(t, home, colID, &domain.Request{
				Name:     "Submit",
				Method:   http.MethodPost,
				URL:      srv.URL + "/submit",
				Body:     tc.body,
				BodyType: tc.bodyType,
			})

			_, stderr, code := runQuarkWithHome(t, home, "run", "API/Submit")
			require.Equal(t, 0, code, "stderr: %s", stderr)
			contentType, payload := got.last()
			assert.Equal(t, tc.wantType, contentType)
			assert.Equal(t, tc.wantPayload, payload)
		})
	}
}

// A Postman urlencoded body is stored as a form body, so Quark escapes its
// variables and sends the right Content-Type.
func TestE2E_PostmanImport_UrlencodedBodyBecomesFormBody(t *testing.T) {
	home := t.TempDir()
	dbPath := createQuarkDBPath(t, home)
	st, err := store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	require.NoError(t, st.Close())

	exportDir := filepath.Join(t.TempDir(), "postman-export")
	require.NoError(t, os.MkdirAll(filepath.Join(exportDir, "collection"), 0o700))
	writeJSONFile(t, filepath.Join(exportDir, "collection", "form.json"), map[string]any{
		"info": map[string]any{
			"name":   "Form API",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		"item": []map[string]any{{
			"name": "Form post",
			"request": map[string]any{
				"method": "POST",
				"url":    map[string]any{"raw": "https://api.example.test/form"},
				"body": map[string]any{
					"mode": "urlencoded",
					"urlencoded": []map[string]any{
						{"key": "a", "value": "1"},
						{"key": "b", "value": "x y"},
					},
				},
			},
		}},
	})

	stdout, stderr, code := runQuarkWithHome(t, home, "import-postman", exportDir)
	require.Equal(t, 0, code, "import failed: %s %s", stdout, stderr)

	st, err = store.New(dbPath, store.WithCacheSize(100))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	cols, err := st.ListCollections(ctx)
	require.NoError(t, err)
	var found *domain.Request
	for _, col := range cols {
		reqs, listErr := st.ListRequests(ctx, col.ID)
		require.NoError(t, listErr)
		for _, req := range reqs {
			if req.Name == "Form post" {
				found = req
			}
		}
	}
	require.NotNil(t, found, "the imported request must exist")
	assert.Equal(t, domain.BodyTypeFormURLEncoded, found.BodyType)
	assert.Contains(t, found.Body, "a=1")
	assert.Contains(t, found.Body, "b=x+y")
}
