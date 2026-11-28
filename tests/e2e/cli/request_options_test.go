//go:build e2e

package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// curl import reads per-request options: timeout, redirects, proxy and TLS
// verification. The options are stored with the request and shown on import.
func TestCLI_ImportCurlOptionsAreStoredAndShown(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	command := "curl -L --max-redirs 3 -k --max-time 2.5 -x http://proxy.example.test:3128 " +
		"https://api.example.test/items"

	out, errOut, code := runQuarkWithHome(t, home, "import", "curl", command,
		"--collection", colID, "--name", "With options")
	require.Equal(t, 0, code, "import failed: %s %s", out, errOut)
	assert.Contains(t, out, "Options:")
	assert.Contains(t, out, "timeout 2.5s")
	assert.Contains(t, out, "max 3 redirects")
	assert.Contains(t, out, "TLS verification off")
	assert.Contains(t, out, "proxy http://proxy.example.test:3128")

	st, err := store.New(filepath.Join(home, ".quark", "quark.db"))
	require.NoError(t, err)
	requests, err := st.ListRequests(context.Background(), colID)
	require.NoError(t, err)
	require.NoError(t, st.Close())
	require.Len(t, requests, 1)
	got := requests[0].Options
	assert.Equal(t, 2500, got.TimeoutMS)
	require.NotNil(t, got.FollowRedirects)
	assert.True(t, *got.FollowRedirects)
	require.NotNil(t, got.MaxRedirects)
	assert.Equal(t, 3, *got.MaxRedirects)
	assert.True(t, got.SkipTLSVerify)
	assert.Equal(t, "http://proxy.example.test:3128", got.Proxy)
}

func TestCLI_ImportCurlRejectsBadMaxTime(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	out, errOut, code := runQuarkWithHome(t, home, "import", "curl",
		"curl --max-time soon https://api.example.test/", "--collection", colID, "--name", "Bad")
	assert.NotEqual(t, 0, code, "import must fail: %s", out)
	assert.Contains(t, errOut+out, "--max-time needs a number of seconds")
}

// Export writes each option back as the curl flag that sets it.
func TestCLI_RequestExportCurlWritesOptionFlags(t *testing.T) {
	tests := []struct {
		name     string
		options  domain.RequestOptions
		wantAll  []string
		wantNone []string
	}{
		{
			name:    "redirects off",
			options: domain.RequestOptions{FollowRedirects: boolP(false)},
			wantAll: []string{"-L --max-redirs 0"},
		},
		{
			name:    "redirects on with a limit",
			options: domain.RequestOptions{FollowRedirects: boolP(true), MaxRedirects: intP(4)},
			wantAll: []string{"-L", "--max-redirs 4"},
		},
		{
			name: "timeout, TLS, and a socks proxy",
			options: domain.RequestOptions{
				TimeoutMS:     1500,
				SkipTLSVerify: true,
				Proxy:         "socks5://proxy.example.test:1080",
			},
			wantAll: []string{"-k", "-x 'socks5://proxy.example.test:1080'", "--max-time 1.5"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			seedRequest(t, home, colID, &domain.Request{
				Name:    "Opts",
				Method:  http.MethodGet,
				URL:     "https://api.example.test/opts",
				Options: tc.options,
			})
			out, errOut, code := runQuarkWithHome(t, home, "request", "export-curl", "API/Opts")
			require.Equal(t, 0, code, "export failed: %s %s", out, errOut)
			for _, want := range tc.wantAll {
				assert.Contains(t, out, want)
			}
		})
	}
}

func boolP(v bool) *bool { return &v }
func intP(v int) *int    { return &v }

// A proxy option sends the request through that proxy, which receives the
// absolute URL.
func TestCLI_RunRequestOptionProxyCarriesRequest(t *testing.T) {
	var mu sync.Mutex
	var seen string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.URL.String()
		mu.Unlock()
		_, _ = io.WriteString(w, "via proxy")
	}))
	t.Cleanup(proxy.Close)

	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:    "Proxied",
		Method:  http.MethodGet,
		URL:     "http://origin.example.test/proxied",
		Options: domain.RequestOptions{Proxy: proxy.URL},
	})

	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Proxied")
	require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
	assert.Contains(t, out, "via proxy")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "http://origin.example.test/proxied", seen)
}

// skip TLS verify applies only to the request that sets it.
func TestCLI_RunRequestOptionSkipTLSVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "tls ok")
	}))
	t.Cleanup(srv.Close)

	t.Run("accepted when the request sets it", func(t *testing.T) {
		home, colID := setupHomeWithCollection(t)
		seedRequest(t, home, colID, &domain.Request{
			Name: "Insecure", Method: http.MethodGet, URL: srv.URL,
			Options: domain.RequestOptions{SkipTLSVerify: true},
		})
		out, errOut, code := runQuarkWithHome(t, home, "run", "API/Insecure")
		require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
		assert.Contains(t, out, "tls ok")
	})

	t.Run("refused without it", func(t *testing.T) {
		home, colID := setupHomeWithCollection(t)
		seedRequest(t, home, colID, &domain.Request{
			Name: "Strict", Method: http.MethodGet, URL: srv.URL,
		})
		out, errOut, code := runQuarkWithHome(t, home, "run", "API/Strict")
		assert.NotEqual(t, 0, code, "a self-signed certificate must be rejected: %s", out)
		assert.Contains(t, errOut+out, "certificate")
	})
}

// A request timeout option cuts off a response that is slower than it.
func TestCLI_RunRequestOptionTimeoutCutsOffSlowResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	}))
	t.Cleanup(srv.Close)

	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Slow", Method: http.MethodGet, URL: srv.URL,
		Options: domain.RequestOptions{TimeoutMS: 50},
	})
	out, errOut, code := runQuarkWithHome(t, home, "run", "API/Slow")
	assert.NotEqual(t, 0, code, "the 50ms timeout must stop the request: %s", out)
	assert.NotContains(t, out, "late")
	assert.Contains(t, errOut+out, "timed out")
}

// Redirect options: "do not follow" keeps the 302 and its body, and a limit of
// one stops a two-hop chain at the second 302.
func TestCLI_RunRequestOptionRedirectLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/one":
			w.Header().Set("Location", "/two")
			w.WriteHeader(http.StatusFound)
			_, _ = io.WriteString(w, "hop one")
		case "/two":
			w.Header().Set("Location", "/end")
			w.WriteHeader(http.StatusFound)
			_, _ = io.WriteString(w, "hop two")
		default:
			_, _ = io.WriteString(w, "end reached")
		}
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name    string
		options domain.RequestOptions
		want    string
		notWant string
	}{
		{
			name:    "do not follow",
			options: domain.RequestOptions{FollowRedirects: boolP(false)},
			want:    "hop one",
			notWant: "end reached",
		},
		{
			name:    "limit of one redirect",
			options: domain.RequestOptions{FollowRedirects: boolP(true), MaxRedirects: intP(1)},
			want:    "hop two",
			notWant: "end reached",
		},
		{
			name:    "follow with the default limit",
			options: domain.RequestOptions{FollowRedirects: boolP(true)},
			want:    "end reached",
			notWant: "hop two",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home, colID := setupHomeWithCollection(t)
			seedRequest(t, home, colID, &domain.Request{
				Name: "Hops", Method: http.MethodGet, URL: srv.URL + "/one", Options: tc.options,
			})
			out, errOut, code := runQuarkWithHome(t, home, "run", "API/Hops")
			require.Equal(t, 0, code, "run failed: %s %s", out, errOut)
			assert.Contains(t, out, tc.want)
			assert.NotContains(t, out, tc.notWant)
		})
	}
}
