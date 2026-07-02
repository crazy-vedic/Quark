//go:build e2e

// Run with: go test -tags e2e ./internal/cli/...
package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

type e2eRecordingRoundTripper struct {
	lastMethod  string
	lastURL     string
	lastBody    string
	lastHeaders http.Header
	response    *http.Response
}

func (rt *e2eRecordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body := []byte(nil)
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}
	rt.lastMethod = req.Method
	rt.lastURL = req.URL.String()
	rt.lastBody = string(body)
	rt.lastHeaders = req.Header.Clone()
	return rt.response, nil
}

func TestE2E_RunCommand_UsesPositionalsNamedVarsAndStoredEnvs(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "cli-e2e.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))

	defaultEnv, err := st.CreateDefaultEnvironment(ctx, col.ID)
	require.NoError(t, err)
	defaultVars := defaultEnv.Vars()
	if defaultVars == nil {
		defaultVars = make(map[string]string)
	}
	defaultVars["merchant_id"] = "env-merchant"
	defaultVars["token"] = "env-token"
	defaultEnv.SetVars(defaultVars)
	require.NoError(t, st.SaveEnvironment(ctx, defaultEnv))
	require.NoError(t, st.SetActiveEnvironment(ctx, col.ID, defaultEnv.ID))

	global, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	globalVars := global.Vars()
	if globalVars == nil {
		globalVars = make(map[string]string)
	}
	globalVars["name"] = "global-name"
	global.SetVars(globalVars)
	require.NoError(t, st.SaveEnvironment(ctx, global))

	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "create-item",
		Method:       "POST",
		URL:          "https://example.test/{{1|merchant_id}}",
		Body:         `{"merchant_id":"{{1|merchant_id}}","name":"{{name}}"}`,
		Headers:      `{"Authorization":"Bearer {{token}}"}`,
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	transport := &e2eRecordingRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
		},
	}
	executor := exec.New(transport)

	cmd := NewRunCmd(st, executor)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"API/create-item",
		"pos-123",
		"--var", "name=cli-name",
		"--var", "token=cli-token",
	})
	cmd.SetContext(ctx)

	err = cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, "POST", transport.lastMethod)
	assert.Equal(t, "https://example.test/pos-123", transport.lastURL)
	assert.Equal(t, `{"merchant_id":"pos-123","name":"cli-name"}`, transport.lastBody)
	assert.Equal(t, "Bearer cli-token", transport.lastHeaders.Get("Authorization"))
	assert.Contains(t, out.String(), "Status: 200 OK")
	assert.Contains(t, out.String(), `{"ok":true}`)
}

func TestE2E_RunCommand_FallbackErrorsWhenNoCandidateResolves(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "cli-e2e.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))

	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "create-item",
		Method:       "GET",
		URL:          "https://example.test/{{1|merchant_id}}",
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	transport := &e2eRecordingRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
		},
	}
	executor := exec.New(transport)

	cmd := NewRunCmd(st, executor)
	cmd.SetArgs([]string{"API/create-item"})
	cmd.SetContext(ctx)

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `1|merchant_id`)
}

func TestE2E_RunCommand_UsesStructuredAuth(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "cli-auth-e2e.db"), store.WithCacheSize(100))
	require.NoError(t, err)
	defer st.Close()

	ctx := context.Background()

	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))

	global, err := st.GetGlobalEnvironment(ctx)
	require.NoError(t, err)
	global.SetVars(map[string]string{"token": "secret-token"})
	require.NoError(t, st.SaveEnvironment(ctx, global))

	req := &domain.Request{
		ID:           uuid.New().String(),
		CollectionID: col.ID,
		Name:         "auth",
		Method:       "GET",
		URL:          "https://example.test/users",
		AuthType:     domain.AuthTypeBearer,
		AuthConfig:   `{"token":"{{token}}"}`,
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	transport := &e2eRecordingRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewBufferString("ok")),
		},
	}
	executor := exec.New(transport)

	cmd := NewRunCmd(st, executor)
	cmd.SetArgs([]string{"API/auth"})
	cmd.SetContext(ctx)

	err = cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret-token", transport.lastHeaders.Get("Authorization"))
}
