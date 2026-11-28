//go:build e2e

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// extractionBody is a response with nested values of each JSON type, an array
// and a key that needs quoting in a JSONPath.
const extractionBody = `{"data":{"token":"tok-1","count":3,"ok":true,"nothing":null,` +
	`"obj":{"a":1},"items":[{"id":"first"},{"id":"second"}],"quoted key":"quoted-value"},` +
	`"list":["a","b","c"]}`

// echoHeaderServer records the X-Values header of every request to /echo and
// answers every other path with the given handler.
type echoHeaderServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	values []string
}

func newEchoHeaderServer(t *testing.T, serve http.HandlerFunc) *echoHeaderServer {
	t.Helper()
	e := &echoHeaderServer{}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo" {
			e.mu.Lock()
			e.values = append(e.values, r.Header.Get("X-Values"))
			e.mu.Unlock()
			_, _ = w.Write([]byte("ok"))
			return
		}
		serve(w, r)
	}))
	t.Cleanup(e.srv.Close)
	return e
}

// last returns the X-Values header of the most recent /echo request.
func (e *echoHeaderServer) last(t *testing.T) string {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	require.NotEmpty(t, e.values, "the echo request was never received")
	return e.values[len(e.values)-1]
}

// seedEchoRequest saves a request that sends X-Values built from template.
func seedEchoRequest(t *testing.T, home, colID string, base, template string) {
	t.Helper()
	headers, err := json.Marshal(map[string]string{"X-Values": template})
	require.NoError(t, err)
	seedRequest(t, home, colID, &domain.Request{
		Name:    "echo",
		Method:  http.MethodGet,
		URL:     base + "/echo",
		Headers: string(headers),
	})
}

func seedExtractingRequest(t *testing.T, home, colID, name, url string, rules ...domain.Extraction) {
	t.Helper()
	seedRequest(t, home, colID, &domain.Request{
		Name:        name,
		Method:      http.MethodGet,
		URL:         url,
		Extractions: domain.EncodeExtractions(rules),
	})
}

func TestCLI_RunExtractsJSONPathValuesForLaterRequests(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEchoHeaderServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(extractionBody))
	})
	rules := []domain.Extraction{
		{Variable: "token", Source: domain.ExtractFromBody, Path: "$.data.token"},
		{Variable: "count", Source: domain.ExtractFromBody, Path: "data.count"},
		{Variable: "flag", Source: domain.ExtractFromBody, Path: "$.data.ok"},
		{Variable: "first", Source: domain.ExtractFromBody, Path: "$.data.items[0].id"},
		{Variable: "last", Source: domain.ExtractFromBody, Path: "$.list[-1]"},
		{Variable: "quoted", Source: domain.ExtractFromBody, Path: "$['data']['quoted key']"},
		{Variable: "obj", Source: domain.ExtractFromBody, Path: `$["data"]["obj"]`},
		{Variable: "nul", Source: domain.ExtractFromBody, Path: "$.data.nothing"},
		{Variable: "missing", Source: domain.ExtractFromBody, Path: "$.data.nope"},
		{Variable: "e_bad", Source: domain.ExtractFromBody, Path: "$.["},
		{Variable: "e_index", Source: domain.ExtractFromBody, Path: "$.data.items[abc]"},
		{Variable: "e_quote", Source: domain.ExtractFromBody, Path: "$['data"},
		{Variable: "e_space", Source: domain.ExtractFromBody, Path: "$.data token"},
		{Variable: "e_empty_index", Source: domain.ExtractFromBody, Path: "$[]"},
		{Variable: "e_member_of_array", Source: domain.ExtractFromBody, Path: "$.list.name"},
		{Variable: "e_index_of_string", Source: domain.ExtractFromBody, Path: "$.data.token[0]"},
		{Variable: "e_out_of_range", Source: domain.ExtractFromBody, Path: "$.data.items[5].id"},
	}
	seedExtractingRequest(t, home, colID, "login", srv.srv.URL+"/login", rules...)

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/login")
	require.Equal(t, 0, code, "stdout: %s stderr: %s", out, errOut)
	assert.Contains(t, errOut, "Extracted: token -> default")
	assert.Contains(t, errOut, "Extracted: count -> default")
	assert.Contains(t, errOut, "Extracted: obj -> default")
	assert.Contains(t, errOut, "Extract failed: missing:")
	assert.Contains(t, errOut, "Extract failed: e_bad:")
	assert.Contains(t, errOut, "Extract failed: e_index:")
	assert.Contains(t, errOut, "Extract failed: e_quote:")
	assert.Contains(t, errOut, "Extract failed: e_space:")
	assert.Contains(t, errOut, "Extract failed: e_empty_index:")
	assert.Contains(t, errOut, "Extract failed: e_member_of_array:")
	assert.Contains(t, errOut, "Extract failed: e_index_of_string:")
	assert.Contains(t, errOut, "Extract failed: e_out_of_range:")

	seedEchoRequest(t, home, colID, srv.srv.URL,
		"{{token}}|{{count}}|{{flag}}|{{first}}|{{last}}|{{quoted}}|{{obj}}|[{{nul}}]")
	_, errOut, code = runQuarkWithHome(t, home, "run", "API/echo")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Equal(t,
		`tok-1|3|true|first|c|quoted-value|{"a":1}|[null]`,
		srv.last(t))
}

func TestCLI_RunExtractsHeadersAndReportsMissingOnes(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEchoHeaderServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("X-Req-Id", "req-77")
		w.Header().Add("X-Multi", "one")
		w.Header().Add("X-Multi", "two")
		_, _ = w.Write([]byte("no json here"))
	})
	seedExtractingRequest(t, home, colID, "headers", srv.srv.URL+"/headers",
		domain.Extraction{Variable: "rid", Source: domain.ExtractFromHeader, Path: "X-Req-Id"},
		domain.Extraction{Variable: "multi", Source: domain.ExtractFromHeader, Path: "X-Multi"},
		domain.Extraction{Variable: "nohdr", Source: domain.ExtractFromHeader, Path: "X-Missing"},
		domain.Extraction{Variable: "notjson", Source: domain.ExtractFromBody, Path: "$.a"},
	)

	_, errOut, code := runQuarkWithHome(t, home, "run", "API/headers")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, errOut, "Extracted: rid -> default")
	assert.Contains(t, errOut, "Extracted: multi -> default")
	assert.Contains(t, errOut, `Extract failed: nohdr: response has no "X-Missing" header`)
	assert.Contains(t, errOut, "Extract failed: notjson:")

	seedEchoRequest(t, home, colID, srv.srv.URL, "{{rid}}|{{multi}}")
	_, errOut, code = runQuarkWithHome(t, home, "run", "API/echo")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Equal(t, "req-77|one, two", srv.last(t))
}

func TestCLI_RunSkipsExtractionForErrorStatus(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEchoHeaderServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"token":"should-not-be-stored"}`))
	})
	seedExtractingRequest(t, home, colID, "gone", srv.srv.URL+"/gone",
		domain.Extraction{Variable: "token", Source: domain.ExtractFromBody, Path: "$.token"},
	)

	_, errOut, code := runQuarkWithHome(t, home, "run", "API/gone")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, errOut, "Extract skipped: token (extraction skipped: HTTP 404 is not a 2xx response)")
	assert.NotContains(t, errOut, "Extracted: token")

	// The variable was never written, so a request that uses it fails to
	// resolve it and is not sent.
	seedEchoRequest(t, home, colID, srv.srv.URL, "{{token}}")
	_, errOut, code = runQuarkWithHome(t, home, "run", "API/echo")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, `unresolved variable: "token"`)
}

func TestCLI_RunReportsInvalidExtractionRules(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEchoHeaderServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a":1}`))
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "badrules", Method: http.MethodGet, URL: srv.srv.URL + "/badrules",
		Extractions: "not json",
	})
	seedExtractingRequest(t, home, colID, "unknown", srv.srv.URL+"/unknown",
		domain.Extraction{Variable: "x", Source: "xml", Path: "$.a"},
	)

	_, errOut, code := runQuarkWithHome(t, home, "run", "API/badrules")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, errOut, "extraction rules are not valid JSON")

	_, errOut, code = runQuarkWithHome(t, home, "run", "API/unknown")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, errOut, `Extract failed: x: unknown extraction source "xml"`)
}

func TestCLI_RunWritesExtractedValuesToTheActiveEnvironment(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := newEchoHeaderServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"dev-token"}`))
	})

	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	require.NoError(t, st.SetActiveEnvironment(context.Background(), colID, "env-dev"))
	require.NoError(t, st.Close())

	seedExtractingRequest(t, home, colID, "login", srv.srv.URL+"/login",
		domain.Extraction{Variable: "token", Source: domain.ExtractFromBody, Path: "$.token"},
	)
	_, errOut, code := runQuarkWithHome(t, home, "run", "API/login")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, errOut, "Extracted: token -> dev")

	seedEchoRequest(t, home, colID, srv.srv.URL, "{{token}}")
	_, errOut, code = runQuarkWithHome(t, home, "run", "API/echo")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Equal(t, "dev-token", srv.last(t))
}

func TestCLI_RunExtractsFromBodyStreamedToDisk(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	// Larger than the 10 MiB in-memory threshold, so the body is read back
	// from its temp file for extraction.
	padding := strings.Repeat("p", 11<<20)
	srv := newEchoHeaderServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"big":%q}`, padding)
	})
	seedExtractingRequest(t, home, colID, "big", srv.srv.URL+"/big",
		domain.Extraction{Variable: "big", Source: domain.ExtractFromBody, Path: "$.big"},
	)

	_, errOut, code := runQuarkWithHome(t, home, "run", "API/big")
	require.Equal(t, 0, code, "stderr: %s", errOut)
	assert.Contains(t, errOut, "Extracted: big -> default")
}
