//go:build e2e

package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// seedRequest saves a request directly so tests can cover executor paths that
// the curl importer would otherwise normalize away.
func seedRequest(t *testing.T, home, colID string, req *domain.Request) {
	t.Helper()
	st, err := store.New(filepath.Join(home, ".quark", "quark.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()
	req.ID = uuid.New().String()
	req.CollectionID = colID
	require.NoError(t, st.SaveRequest(context.Background(), req))
}

// capturedRequest records what the test server received on the wire.
type capturedRequest struct {
	mu       sync.Mutex
	host     string
	trace    string
	transfer []string
	body     string
}

func newCapturingServer(t *testing.T) (*httptest.Server, *capturedRequest) {
	t.Helper()
	got := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		defer got.mu.Unlock()
		got.host = r.Host
		got.trace = r.Header.Get("X-Trace")
		got.transfer = r.TransferEncoding
		got.body = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestCLI_RunSendsFileBackedHeaderAndBodyWithTransportHeaders(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	dir := t.TempDir()
	traceFile := filepath.Join(dir, "trace.txt")
	bodyFile := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(traceFile, []byte("trace-123"), 0o600))
	require.NoError(t, os.WriteFile(bodyFile, []byte(`{"a":1}`), 0o600))
	srv, got := newCapturingServer(t)

	seedRequest(t, home, colID, &domain.Request{
		Name:    "Transport",
		Method:  http.MethodPost,
		URL:     srv.URL + "/echo",
		Headers: `{"Host":["api.internal.test"],"X-Trace":["@` + traceFile + `"],"Transfer-Encoding":["chunked"],"Connection":["close"],"Content-Type":["application/json"]}`,
		Body:    "@" + bodyFile,
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Transport")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)

	got.mu.Lock()
	defer got.mu.Unlock()
	assert.Equal(t, "api.internal.test", got.host)
	assert.Equal(t, "trace-123", got.trace)
	assert.Equal(t, []string{"chunked"}, got.transfer)
	assert.JSONEq(t, `{"a":1}`, got.body)
}

func TestCLI_RunContentLengthHeaderIsApplied(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv, got := newCapturingServer(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:    "Length",
		Method:  http.MethodPost,
		URL:     srv.URL + "/len",
		Headers: `{"Content-Length":["5"]}`,
		Body:    "hello",
	})

	_, errOut, code := runQuarkWithHome(t, home, "run", "API/Length")
	require.Equal(t, 0, code, "run failed: %s", errOut)

	got.mu.Lock()
	defer got.mu.Unlock()
	assert.Equal(t, "hello", got.body)
}

func TestCLI_RunRejectsInvalidTransportHeaders(t *testing.T) {
	cases := []struct {
		name    string
		headers string
		body    string
		want    string
	}{
		{
			name:    "invalid content length",
			headers: `{"Content-Length":["nope"]}`,
			body:    "x",
			want:    "invalid Content-Length",
		},
		{
			name:    "unsupported transfer encoding",
			headers: `{"Transfer-Encoding":["gzip"]}`,
			body:    "x",
			want:    "unsupported Transfer-Encoding",
		},
		{
			name:    "missing header file",
			headers: `{"X-Trace":["@/does/not/exist"]}`,
			body:    "",
			want:    "exec: header",
		},
		{
			name:    "invalid json body",
			headers: `{"Content-Type":["application/json"]}`,
			body:    "{bad",
			want:    "invalid JSON body",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			srv, _ := newCapturingServer(t)
			seedRequest(t, home, colID, &domain.Request{
				Name:    "Bad",
				Method:  http.MethodPost,
				URL:     srv.URL,
				Headers: tc.headers,
				Body:    tc.body,
			})

			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Bad")
			require.NotEqual(t, 0, code)
			assert.Contains(t, out+errOut, tc.want)
		})
	}
}

func TestCLI_RunMissingBodyFileFails(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv, _ := newCapturingServer(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:   "MissingBody",
		Method: http.MethodPost,
		URL:    srv.URL,
		Body:   "@" + filepath.Join(t.TempDir(), "absent.json"),
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/MissingBody")
	require.NotEqual(t, 0, code)
	assert.Contains(t, out+errOut, "read body file")
}

func TestCLI_ImportCurlReadsDataFileBody(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	bodyFile := filepath.Join(t.TempDir(), "form.txt")
	require.NoError(t, os.WriteFile(bodyFile, []byte("name=quark"), 0o600))

	out, errOut, code := runQuarkWithHome(t, home, "import", "curl",
		"curl -X POST 'https://example.test/form' --data-binary @"+bodyFile,
		"--collection", colID, "--name", "FileBody")
	require.Equal(t, 0, code, "import failed: %s %s", out, errOut)

	st, err := store.New(filepath.Join(home, ".quark", "quark.db"))
	require.NoError(t, err)
	defer st.Close()
	requests, err := st.ListRequests(context.Background(), colID)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "name=quark", requests[0].Body)
}

func TestCLI_ImportCurlRejectsUnsafeBodyFiles(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big.bin")
	require.NoError(t, os.WriteFile(big, make([]byte, 10<<20+1), 0o600))
	cases := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "stdin body",
			command: "curl -X POST 'https://example.test' --data-binary @-",
			want:    "stdin-backed request bodies",
		},
		{
			name:    "empty path",
			command: "curl -X POST 'https://example.test' --data-binary @",
			want:    "empty body file path",
		},
		{
			name:    "missing file",
			command: "curl -X POST 'https://example.test' --data-binary @/does/not/exist",
			want:    "read body file",
		},
		{
			name:    "oversized file",
			command: "curl -X POST 'https://example.test' --data-binary @" + big,
			want:    "exceeds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			out, errOut, code := runQuarkWithHome(t, home, "import", "curl", tc.command,
				"--collection", colID, "--name", "Unsafe")
			require.NotEqual(t, 0, code)
			assert.True(t, strings.Contains(out+errOut, tc.want), "output: %s %s", out, errOut)
		})
	}
}

func TestCLI_SearchEmptyQueryListsRequestsAlphabetically(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Zebra", Method: "GET", URL: "https://example.test/z",
	})
	seedRequest(t, home, colID, &domain.Request{
		Name: "Alpha", Method: "GET", URL: "https://example.test/a",
	})

	out, errOut, code := runQuarkWithHome(t, home, "search", "")
	require.Equal(t, 0, code, "search failed: %s", errOut)
	require.Contains(t, out, "Alpha")
	require.Contains(t, out, "Zebra")
	assert.Less(t, strings.Index(out, "Alpha"), strings.Index(out, "Zebra"))
}

func TestCLI_ConfigLastRequestIDIsLoaded(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	cfg := "[ui]\nlast_request_id = \"missing-request\"\n"
	cfgPath := filepath.Join(home, ".quark", "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	_, errOut, code := runQuarkWithHome(t, home, "search", "anything")
	require.Equal(t, 0, code, "search with last_request_id config failed: %s", errOut)
}
