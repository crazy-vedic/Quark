package exec_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

type recordingExecutionWriter struct {
	executions []*domain.Execution
}

func (w *recordingExecutionWriter) SaveExecution(_ context.Context, ex *domain.Execution) error {
	w.executions = append(w.executions, ex)
	return nil
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// --- Helper ---

func newTestExecutor(transport http.RoundTripper, opts ...exec.Option) *exec.Executor {
	return exec.New(transport, opts...)
}

func newTestRequest(method, url, body string) *domain.Request {
	return &domain.Request{
		ID:     "test-req",
		Method: method,
		URL:    url,
		Body:   body,
	}
}

// --- Tests ---

func TestExecutor_GET200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport)
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
	assert.Equal(t, `{"ok":true}`, string(result.Body))
	assert.Empty(t, result.TempPath)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestExecutor_POST_WithBody(t *testing.T) {
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		received = string(b)
		w.WriteHeader(200)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport)
	result, err := e.Execute(context.Background(),
		newTestRequest("POST", srv.URL, `{"name":"test"}`))

	require.NoError(t, err)
	assert.Equal(t, `{"name":"test"}`, received)
	assert.Equal(t, 200, result.StatusCode)
}

func TestExecutor_RequestHeadersSent(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := &domain.Request{
		ID:      "test-req",
		Method:  "GET",
		URL:     srv.URL,
		Headers: `{"Authorization":"Bearer tok123"}`,
	}

	e := newTestExecutor(transport)
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok123", receivedAuth)
}

func TestExecutor_RequestAuth_BearerOverridesManualAuthorization(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := &domain.Request{
		ID:         "test-req",
		Method:     "GET",
		URL:        srv.URL,
		Headers:    `{"Authorization":"Bearer old"}`,
		AuthType:   domain.AuthTypeBearer,
		AuthConfig: `{"token":"new-token"}`,
	}

	e := newTestExecutor(transport)
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "Bearer new-token", receivedAuth)
}

func TestExecutor_RequestAuth_Basic(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := &domain.Request{
		ID:         "test-req",
		Method:     "GET",
		URL:        srv.URL,
		AuthType:   domain.AuthTypeBasic,
		AuthConfig: `{"username":"alice","password":"secret"}`,
	}

	e := newTestExecutor(transport)
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(
		t,
		"Basic "+base64.StdEncoding.EncodeToString([]byte("alice:secret")),
		receivedAuth,
	)
}

func TestExecutor_RequestAuth_APIKeyHeader(t *testing.T) {
	var receivedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-API-Key")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := &domain.Request{
		ID:         "test-req",
		Method:     "GET",
		URL:        srv.URL,
		AuthType:   domain.AuthTypeAPIKey,
		AuthConfig: `{"in":"header","name":"X-API-Key","value":"secret"}`,
	}

	e := newTestExecutor(transport)
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "secret", receivedKey)
}

func TestExecutor_RequestAuth_APIKeyQuery(t *testing.T) {
	var receivedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.URL.Query().Get("api_key")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := &domain.Request{
		ID:         "test-req",
		Method:     "GET",
		URL:        srv.URL,
		AuthType:   domain.AuthTypeAPIKey,
		AuthConfig: `{"in":"query","name":"api_key","value":"secret"}`,
	}

	e := newTestExecutor(transport)
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "secret", receivedToken)
}

func TestExecutor_RequestAuth_InvalidConfigFailsBuild(t *testing.T) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := &domain.Request{
		ID:         "test-req",
		Method:     "GET",
		URL:        "https://example.com",
		AuthType:   domain.AuthTypeAPIKey,
		AuthConfig: `{"in":"query","value":"secret"}`,
	}

	e := newTestExecutor(transport)
	result, err := e.Execute(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "api key name is required")
}

func TestExecutor_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithTimeout(50*time.Millisecond))
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.Is(err, exec.ErrTimeout), "expected ErrTimeout, got: %v", err)
}

func TestExecutor_ContextCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	ctx, cancel := context.WithCancel(context.Background())
	e := newTestExecutor(transport)

	done := make(chan error, 1)
	go func() {
		<-started
		cancel()
	}()

	go func() {
		_, err := e.Execute(ctx, newTestRequest("GET", srv.URL, ""))
		done <- err
	}()

	err := <-done
	require.Error(t, err)
	assert.True(
		t,
		errors.Is(err, exec.ErrRequestCancelled),
		"expected ErrRequestCancelled, got: %v",
		err,
	)
}

func TestExecutor_4xxIsNotGoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport)
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err, "4xx must not be a Go error")
	assert.Equal(t, 404, result.StatusCode)
}

func TestExecutor_5xxIsNotGoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport)
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err, "5xx must not be a Go error")
	assert.Equal(t, 500, result.StatusCode)
}

func TestExecutor_PersistsExecutionHistory_OnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	writer := &recordingExecutionWriter{}
	e := newTestExecutor(transport, exec.WithExecutionWriter(writer))
	_, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	require.Len(t, writer.executions, 1)
	assert.Equal(t, "test-req", writer.executions[0].RequestID)
	assert.Equal(t, 200, writer.executions[0].StatusCode)
	assert.Contains(t, writer.executions[0].ResponseBody, `"ok":true`)
}

func TestExecutor_PersistsExecutionHistory_WithStructuredAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	writer := &recordingExecutionWriter{}
	e := newTestExecutor(transport, exec.WithExecutionWriter(writer))
	req := &domain.Request{
		ID:         "test-req",
		Method:     "GET",
		URL:        srv.URL,
		AuthType:   domain.AuthTypeBearer,
		AuthConfig: `{"token":"secret-token"}`,
	}
	_, err := e.Execute(context.Background(), req)

	require.NoError(t, err)
	require.Len(t, writer.executions, 1)
	assert.Contains(t, writer.executions[0].RequestSnapshot, `"auth_type":"bearer"`)
	assert.Contains(
		t,
		writer.executions[0].RequestSnapshot,
		`"auth_config":"{\"token\":\"secret-token\"}"`,
	)
}

func TestExecutor_PersistsExecutionHistory_OnFailure(t *testing.T) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	writer := &recordingExecutionWriter{}
	e := newTestExecutor(transport, exec.WithExecutionWriter(writer))
	_, err := e.Execute(context.Background(), newTestRequest("GET", "://bad-url", ""))

	require.Error(t, err)
	require.Len(t, writer.executions, 1)
	assert.Equal(t, "test-req", writer.executions[0].RequestID)
	assert.Equal(t, 0, writer.executions[0].StatusCode)
	assert.Contains(t, writer.executions[0].Error, "invalid URL")
}

func TestExecutor_LargeResponse_StreamedToTempFile(t *testing.T) {
	// 600KB response with a 512KB threshold
	bigBody := strings.Repeat("x", 600*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, bigBody)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithMaxResponseSize(512*1024))
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	assert.Nil(t, result.Body, "body must be nil when streamed to temp file")
	assert.NotEmpty(t, result.TempPath, "TempPath must be set for large response")
	assert.Equal(t, int64(len(bigBody)), result.Size)
}

func TestExecutor_HeadersCaseInsensitiveAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a server returning a non-canonical header key
		w.Header()["content-type"] = []string{"application/json"}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport)
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	// http.Header.Get performs canonical lookup — case-insensitive
	ct := http.Header(result.Headers).Get("content-type")
	assert.Equal(t, "application/json", ct)
}

func TestExecutor_DNSError(t *testing.T) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithTimeout(2*time.Second))
	result, err := e.Execute(context.Background(),
		newTestRequest("GET", "http://this-host-does-not-exist.invalid/", ""))

	require.Error(t, err)
	assert.Nil(t, result)
}

func TestExecutor_SizeTracked(t *testing.T) {
	body := `{"hello":"world"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport)
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	assert.Equal(t, int64(len(body)), result.Size)
}

// --- VariableResolver tests ---

func TestExecutor_VariableResolver_SubstitutesURL(t *testing.T) {
	var receivedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resolver := func(_ string) (map[string]string, map[string]string) {
		return map[string]string{"path": "/api/v1/users"}, nil
	}

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithVariableResolver(resolver))
	req := &domain.Request{
		ID:     "test",
		Method: "GET",
		URL:    srv.URL + "{{path}}",
	}
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/users", receivedURL)
}

func TestExecutor_VariableResolver_SubstitutesBody(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resolver := func(_ string) (map[string]string, map[string]string) {
		return map[string]string{"name": "Alice"}, nil
	}

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithVariableResolver(resolver))
	req := &domain.Request{
		ID:     "test",
		Method: "POST",
		URL:    srv.URL,
		Body:   `{"name":"{{name}}"}`,
	}
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, `{"name":"Alice"}`, receivedBody)
}

func TestExecutor_VariableResolver_SubstitutesHeaders(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resolver := func(_ string) (map[string]string, map[string]string) {
		return map[string]string{"token": "secret123"}, nil
	}

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithVariableResolver(resolver))
	req := &domain.Request{
		ID:      "test",
		Method:  "GET",
		URL:     srv.URL,
		Headers: `{"Authorization":"Bearer {{token}}"}`,
	}
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret123", receivedAuth)
}

func TestExecutor_VariableResolver_GlobalFallback(t *testing.T) {
	var receivedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()

	resolver := func(_ string) (map[string]string, map[string]string) {
		return nil, map[string]string{"path": "/global/users"}
	}

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithVariableResolver(resolver))
	req := &domain.Request{
		ID:     "test",
		Method: "GET",
		URL:    srv.URL + "{{path}}",
	}
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "/global/users", receivedURL)
}

func TestExecutor_VariableResolver_UnresolvedError(t *testing.T) {
	resolver := func(_ string) (map[string]string, map[string]string) {
		return nil, nil
	}

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport, exec.WithVariableResolver(resolver))
	req := &domain.Request{
		ID:     "test",
		Method: "GET",
		URL:    "https://{{host}}/api",
	}
	_, err := e.Execute(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
	assert.Contains(t, err.Error(), "host")
}

func TestExecutor_VariableResolver_NoResolver_NoPlaceholders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	e := newTestExecutor(transport) // no resolver
	req := &domain.Request{
		ID:     "test",
		Method: "GET",
		URL:    srv.URL,
	}
	_, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
}

// --- Hook tests ---

type fakePreHook struct {
	called bool
	mutate func(*domain.Request) *domain.Request
	err    error
}

func (h *fakePreHook) BeforeRequest(
	_ context.Context,
	req *domain.Request,
) (*domain.Request, error) {
	h.called = true
	if h.err != nil {
		return nil, h.err
	}
	if h.mutate != nil {
		return h.mutate(req), nil
	}
	return req, nil
}

type fakePostHook struct {
	called bool
	err    error
}

func (h *fakePostHook) AfterResponse(
	_ context.Context,
	_ *domain.Request,
	_ *exec.ExecuteResult,
) error {
	h.called = true
	return h.err
}

func TestExecutor_PreRequestHook_Fires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	hook := &fakePreHook{}
	e := exec.New(transport, exec.WithPreRequestHooks([]exec.PreRequestHook{hook}))
	_, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	assert.True(t, hook.called, "pre-request hook must fire")
}

func TestExecutor_PreRequestHook_CanMutateRequest(t *testing.T) {
	var receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Injected")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	hook := &fakePreHook{
		mutate: func(req *domain.Request) *domain.Request {
			req.Headers = `{"X-Injected":"from-hook"}`
			return req
		},
	}
	e := exec.New(transport, exec.WithPreRequestHooks([]exec.PreRequestHook{hook}))
	_, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	assert.Equal(t, "from-hook", receivedHeader)
}

func TestExecutor_PreRequestHook_Error_AbortsRequest(t *testing.T) {
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	sentinel := errors.New("auth required")
	hook := &fakePreHook{err: sentinel}
	e := exec.New(transport, exec.WithPreRequestHooks([]exec.PreRequestHook{hook}))
	result, err := e.Execute(context.Background(), newTestRequest("GET", "http://example.com", ""))

	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, sentinel)
}

func TestExecutor_PostResponseHook_Fires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	hook := &fakePostHook{}
	e := exec.New(transport, exec.WithPostResponseHooks([]exec.PostResponseHook{hook}))
	_, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	assert.True(t, hook.called, "post-response hook must fire")
}

func TestExecutor_PostResponseHook_ErrorDoesNotAbortResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	hook := &fakePostHook{err: errors.New("log failed")}
	e := exec.New(transport, exec.WithPostResponseHooks([]exec.PostResponseHook{hook}))
	result, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	// Post-response hook error must not propagate — result is still returned.
	require.NoError(t, err)
	assert.Equal(t, 200, result.StatusCode)
}

func TestExecutor_MultiplePostHooks_AllFire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	h1 := &fakePostHook{err: errors.New("h1 error")}
	h2 := &fakePostHook{}
	e := exec.New(transport, exec.WithPostResponseHooks([]exec.PostResponseHook{h1, h2}))
	_, err := e.Execute(context.Background(), newTestRequest("GET", srv.URL, ""))

	require.NoError(t, err)
	assert.True(t, h1.called, "first post hook must fire")
	assert.True(t, h2.called, "second post hook must fire even when first returns error")
}
