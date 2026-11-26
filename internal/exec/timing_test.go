package exec_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/exec"
)

func TestExecutor_RecordsTimingPhasesOverTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(5 * time.Millisecond)
		fmt.Fprint(w, strings.Repeat("x", 1024))
	}))
	defer srv.Close()

	base, ok := srv.Client().Transport.(*http.Transport)
	require.True(t, ok)
	transport := base.Clone()
	defer transport.CloseIdleConnections()
	// The test certificate names example.com; dial localhost so the DNS
	// phase runs, and verify against the certificate's name.
	transport.TLSClientConfig.ServerName = "example.com"
	url := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	e := exec.New(transport)
	result, err := e.Execute(context.Background(), newTestRequest(http.MethodGet, url, ""))
	require.NoError(t, err)

	tm := result.Timing
	assert.False(t, tm.Reused)
	assert.Greater(t, tm.DNS, time.Duration(0), "dns")
	assert.Greater(t, tm.Connect, time.Duration(0), "connect")
	assert.Greater(t, tm.TLS, time.Duration(0), "tls")
	assert.Greater(t, tm.TTFB, time.Duration(0), "ttfb")
	assert.GreaterOrEqual(t, tm.Download, 5*time.Millisecond, "download")
	assert.LessOrEqual(t, tm.DNS+tm.Connect+tm.TLS+tm.TTFB+tm.Download, result.Duration)

	compact := tm.Compact()
	for _, label := range []string{"dns ", "connect ", "tls ", "ttfb ", "download "} {
		assert.Contains(t, compact, label)
	}
}

func TestExecutor_TimingOnReusedConnectionSkipsDialPhases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	e := exec.New(transport)

	_, err := e.Execute(context.Background(), newTestRequest(http.MethodGet, srv.URL, ""))
	require.NoError(t, err)
	result, err := e.Execute(context.Background(), newTestRequest(http.MethodGet, srv.URL, ""))
	require.NoError(t, err)

	tm := result.Timing
	assert.True(t, tm.Reused)
	assert.Zero(t, tm.DNS)
	assert.Zero(t, tm.Connect)
	assert.Zero(t, tm.TLS)
	assert.Greater(t, tm.TTFB, time.Duration(0))
	assert.Contains(t, tm.Compact(), "reused conn")
	assert.NotContains(t, tm.Compact(), "tls")
}

func TestExecutor_TimingDescribesFinalRedirectHop(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "done")
	}))
	defer final.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer first.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	e := exec.New(transport, exec.WithRedirects(true, 5))
	result, err := e.Execute(context.Background(), newTestRequest(http.MethodGet, first.URL, ""))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	assert.False(t, result.Timing.Reused)
	assert.Greater(t, result.Timing.Connect, time.Duration(0))
}

func TestExecutor_TimingZeroForTransportWithoutTrace(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       http.NoBody,
		}, nil
	})
	result, err := exec.New(transport).
		Execute(context.Background(), newTestRequest(http.MethodGet, "http://example.test", ""))
	require.NoError(t, err)
	assert.True(t, result.Timing.IsZero())
	assert.Empty(t, result.Timing.Compact())
}

func TestTimingCompactFormatting(t *testing.T) {
	tm := exec.Timing{
		DNS:      1500 * time.Microsecond,
		Connect:  400 * time.Microsecond,
		TTFB:     42 * time.Millisecond,
		Download: 12345 * time.Microsecond,
	}
	assert.Equal(t, "dns 1.5ms  connect 400µs  ttfb 42ms  download 12ms", tm.Compact())
}
