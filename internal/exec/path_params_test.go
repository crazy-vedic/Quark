package exec_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestApplyPathParams_FillsStoredValues(t *testing.T) {
	req := &domain.Request{
		URL:    "https://api.test/users/:id?x=1",
		Params: `{"path_values":{"id":"{{user}}"}}`,
	}
	out, err := exec.ApplyPathParams(req)
	require.NoError(t, err)
	assert.Equal(t, "https://api.test/users/{{user}}?x=1", out.URL)
	assert.Equal(t, "https://api.test/users/:id?x=1", req.URL, "input must not change")
}

func TestApplyPathParams_NoPlaceholderSkipsParams(t *testing.T) {
	req := &domain.Request{URL: "https://api.test/items", Params: "{not json"}
	out, err := exec.ApplyPathParams(req)
	require.NoError(t, err)
	assert.Same(t, req, out)
}

func TestApplyPathParams_InvalidParamsRefused(t *testing.T) {
	req := &domain.Request{URL: "https://api.test/users/:id", Params: "{not json"}
	_, err := exec.ApplyPathParams(req)
	assert.ErrorContains(t, err, "not valid JSON")
}

func TestApplyPathParams_NilRequest(t *testing.T) {
	_, err := exec.ApplyPathParams(nil)
	assert.Error(t, err)
}

func TestExecutor_SendsPathParamsWithVariables(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &http.Transport{}
	defer transport.CloseIdleConnections()

	req := newTestRequest("GET", srv.URL+"/users/:id/items?page=2", "")
	req.Params = `{"path_values":{"id":"{{user_id}}"}}`
	prepared, err := exec.InterpolateRequest(req, map[string]string{"user_id": "a b"}, nil)
	require.NoError(t, err)

	e := newTestExecutor(transport)
	_, err = e.Execute(context.Background(), prepared)
	require.NoError(t, err)
	assert.Equal(t, "/users/a%20b/items?page=2", gotPath)
}
