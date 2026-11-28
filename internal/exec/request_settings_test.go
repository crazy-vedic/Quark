package exec_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

// snapshotCapture keeps every history entry the executor writes.
type snapshotCapture struct {
	mu      sync.Mutex
	entries []*domain.Execution
}

func (c *snapshotCapture) SaveExecution(_ context.Context, ex *domain.Execution) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, ex)
	return nil
}

func redirectChainServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hop":
			http.Redirect(w, r, "/hop2", http.StatusFound)
		case "/hop2":
			http.Redirect(w, r, "/done", http.StatusFound)
		default:
			_, _ = w.Write([]byte("done"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withOptions(req *domain.Request, opts domain.RequestOptions) *domain.Request {
	req.Options = opts
	return req
}

func boolRef(v bool) *bool { return &v }
func intRef(v int) *int    { return &v }

func TestRequestOptions_RedirectsOverrideDefaultsPerRequest(t *testing.T) {
	srv := redirectChainServer(t)
	ctx := context.Background()
	e := exec.New(http.DefaultTransport)

	res, err := e.Execute(ctx, newTestRequest("GET", srv.URL+"/hop", ""))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode, "default policy follows redirects")

	res, err = e.Execute(ctx, withOptions(
		newTestRequest("GET", srv.URL+"/hop", ""),
		domain.RequestOptions{FollowRedirects: boolRef(false)},
	))
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, res.StatusCode,
		"follow_redirects=false stops at the first 302")

	res, err = e.Execute(ctx, withOptions(
		newTestRequest("GET", srv.URL+"/hop", ""),
		domain.RequestOptions{MaxRedirects: intRef(1)},
	))
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, res.StatusCode, "two hops need more than max_redirects=1")

	off := exec.New(http.DefaultTransport, exec.WithRedirects(false, 0))
	res, err = off.Execute(ctx, withOptions(
		newTestRequest("GET", srv.URL+"/hop", ""),
		domain.RequestOptions{FollowRedirects: boolRef(true), MaxRedirects: intRef(10)},
	))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode, "a request can turn following on for itself")
}

func TestRequestOptions_TimeoutOverridesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("slow"))
	}))
	t.Cleanup(srv.Close)
	e := exec.New(http.DefaultTransport, exec.WithTimeout(50*time.Millisecond))
	ctx := context.Background()

	_, err := e.Execute(ctx, newTestRequest("GET", srv.URL, ""))
	require.Error(t, err)
	assert.ErrorIs(t, err, exec.ErrTimeout, "the 50ms default is too short")

	res, err := e.Execute(ctx, withOptions(
		newTestRequest("GET", srv.URL, ""),
		domain.RequestOptions{TimeoutMS: 2000},
	))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "slow", string(res.Body))
}

func TestRequestOptions_ProxyOverrideRoutesThroughProxy(t *testing.T) {
	var mu sync.Mutex
	var seen string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r.URL.String()
		mu.Unlock()
		_, _ = w.Write([]byte("via proxy"))
	}))
	t.Cleanup(proxy.Close)
	e := exec.New(http.DefaultTransport)

	res, err := e.Execute(context.Background(), withOptions(
		newTestRequest("GET", "http://target.invalid/path", ""),
		domain.RequestOptions{Proxy: proxy.URL},
	))
	require.NoError(t, err)
	assert.Equal(t, "via proxy", string(res.Body))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "http://target.invalid/path", seen)
}

func TestRequestOptions_SkipTLSVerifyAppliesOnlyToThatRequest(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secure"))
	}))
	t.Cleanup(srv.Close)
	e := exec.New(http.DefaultTransport)
	ctx := context.Background()

	_, err := e.Execute(ctx, newTestRequest("GET", srv.URL, ""))
	require.Error(t, err, "an untrusted certificate fails by default")

	res, err := e.Execute(ctx, withOptions(
		newTestRequest("GET", srv.URL, ""),
		domain.RequestOptions{SkipTLSVerify: true},
	))
	require.NoError(t, err)
	assert.Equal(t, "secure", string(res.Body))

	_, err = e.Execute(ctx, newTestRequest("GET", srv.URL, ""))
	require.Error(t, err, "the override must not leak into later requests")
}

func TestRequestOptions_UnsupportedTransportRejectsProxyButNotRedirects(t *testing.T) {
	calls := 0
	rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})
	e := exec.New(rt)

	_, err := e.Execute(context.Background(), withOptions(
		newTestRequest("GET", "http://example.test/", ""),
		domain.RequestOptions{Proxy: "http://proxy.internal:3128"},
	))
	require.Error(t, err)
	assert.ErrorIs(t, err, exec.ErrUnsupportedOption)
	assert.Zero(t, calls, "an unsupported option must fail before anything is sent")

	res, err := e.Execute(context.Background(), withOptions(
		newTestRequest("GET", "http://example.test/", ""),
		domain.RequestOptions{FollowRedirects: boolRef(false)},
	))
	require.NoError(t, err, "redirect overrides need no transport support")
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestRequestOptions_InvalidProxyIsRefusedBeforeSending(t *testing.T) {
	calls := 0
	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not be reached")
	})
	e := exec.New(rt)

	_, err := e.Execute(context.Background(), withOptions(
		newTestRequest("GET", "http://example.test/", ""),
		domain.RequestOptions{Proxy: "ftp://files.internal"},
	))
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInvalidRequestOptions)
	assert.Zero(t, calls)
}

func TestRequestOptions_HistorySnapshotRecordsOptionsSent(t *testing.T) {
	srv := redirectChainServer(t)
	capture := &snapshotCapture{}
	e := exec.New(http.DefaultTransport, exec.WithExecutionWriter(capture))

	req := withOptions(
		newTestRequest("GET", srv.URL+"/done", ""),
		domain.RequestOptions{TimeoutMS: 1500, SkipTLSVerify: true},
	)
	req.ID = "saved-request"
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)

	plain := newTestRequest("GET", srv.URL+"/done", "")
	plain.ID = "saved-request"
	_, err = e.Execute(context.Background(), plain)
	require.NoError(t, err)

	require.Len(t, capture.entries, 2)
	var withOpts struct {
		Options *domain.RequestOptions `json:"options"`
	}
	require.NoError(t, json.Unmarshal([]byte(capture.entries[0].RequestSnapshot), &withOpts))
	require.NotNil(t, withOpts.Options)
	assert.Equal(t, 1500, withOpts.Options.TimeoutMS)
	assert.True(t, withOpts.Options.SkipTLSVerify)

	assert.NotContains(t, capture.entries[1].RequestSnapshot, `"options"`,
		"requests without options keep the snapshot shape they had before")
}

func TestRequestOptions_SurviveVariableSubstitution(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("resolved"))
	}))
	t.Cleanup(srv.Close)
	// Production sends always interpolate variables first, and that step copies
	// the request. The options must survive the copy.
	e := exec.New(http.DefaultTransport, exec.WithVariableResolver(
		func(string) (map[string]string, map[string]string, error) {
			return map[string]string{"BASE": srv.URL}, nil, nil
		},
	))

	res, err := e.Execute(context.Background(), withOptions(
		newTestRequest("GET", "{{BASE}}/x", ""),
		domain.RequestOptions{SkipTLSVerify: true},
	))
	require.NoError(t, err, "skip_tls_verify must still apply after variable substitution")
	assert.Equal(t, "resolved", string(res.Body))
}
