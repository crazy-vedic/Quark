package exec_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestInterpolateRequest_NilRequest(t *testing.T) {
	_, err := exec.InterpolateRequest(nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil request")
}

func TestInterpolateRequest_NoEnvs_NoPlaceholders(t *testing.T) {
	req := &domain.Request{
		URL:     "https://example.com/api",
		Body:    `{"name": "test"}`,
		Headers: `{"Content-Type": "application/json"}`,
	}
	out, err := exec.InterpolateRequest(req, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, req.URL, out.URL)
	assert.Equal(t, req.Body, out.Body)
	assert.Equal(t, req.Headers, out.Headers)
	assert.NotSame(t, req, out, "output should be a copy, not the same pointer")
}

func TestInterpolateRequest_NoEnvs_WithPlaceholders(t *testing.T) {
	req := &domain.Request{
		URL: "https://{{host}}/api",
	}
	_, err := exec.InterpolateRequest(req, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
}

func TestInterpolateRequest_URL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		colEnv    map[string]string
		globalEnv map[string]string
		want      string
		wantErr   bool
	}{
		{
			name:   "simple substitution",
			url:    "https://{{host}}/api",
			colEnv: map[string]string{"host": "example.com"},
			want:   "https://example.com/api",
		},
		{
			name:      "global fallback",
			url:       "https://{{host}}/api",
			globalEnv: map[string]string{"host": "global.com"},
			want:      "https://global.com/api",
		},
		{
			name:      "collection overrides global",
			url:       "https://{{host}}/api",
			colEnv:    map[string]string{"host": "col.com"},
			globalEnv: map[string]string{"host": "global.com"},
			want:      "https://col.com/api",
		},
		{
			name:   "multiple placeholders",
			url:    "https://{{host}}/{{path}}/{{id}}",
			colEnv: map[string]string{"host": "api.com", "path": "v1", "id": "42"},
			want:   "https://api.com/v1/42",
		},
		{
			name:   "no placeholders",
			url:    "https://example.com/api",
			colEnv: map[string]string{"host": "ignored.com"},
			want:   "https://example.com/api",
		},
		{
			name:    "unresolved",
			url:     "https://{{host}}/api",
			colEnv:  map[string]string{"other": "value"},
			wantErr: true,
		},
		{
			name:   "empty var value",
			url:    "https://{{host}}/api",
			colEnv: map[string]string{"host": ""},
			want:   "https:///api",
		},
		{
			name:   "repeated placeholder",
			url:    "https://{{host}}/api/{{host}}/test",
			colEnv: map[string]string{"host": "example.com"},
			want:   "https://example.com/api/example.com/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &domain.Request{URL: tt.url}
			out, err := exec.InterpolateRequest(req, tt.colEnv, tt.globalEnv)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.URL)
		})
	}
}

func TestInterpolateRequest_Body(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		colEnv    map[string]string
		globalEnv map[string]string
		want      string
		wantErr   bool
	}{
		{
			name:   "simple substitution",
			body:   `{"url": "{{url}}"}`,
			colEnv: map[string]string{"url": "http://localhost/"},
			want:   `{"url": "http://localhost/"}`,
		},
		{
			name:   "multiple substitutions",
			body:   `{"host": "{{host}}", "port": "{{port}}"}`,
			colEnv: map[string]string{"host": "api.com", "port": "8080"},
			want:   `{"host": "api.com", "port": "8080"}`,
		},
		{
			name:   "no placeholders",
			body:   `{"name": "test"}`,
			colEnv: map[string]string{"url": "ignored"},
			want:   `{"name": "test"}`,
		},
		{
			name:    "unresolved",
			body:    `{"url": "{{url}}"}`,
			colEnv:  map[string]string{"other": "value"},
			wantErr: true,
		},
		{
			name:   "empty body",
			body:   "",
			colEnv: map[string]string{"url": "http://localhost/"},
			want:   "",
		},
		{
			name:   "template with special chars",
			body:   `{"url": "{{url}}", "query": "a=1&b=2"}`,
			colEnv: map[string]string{"url": "http://localhost/api?key=val&foo=bar"},
			want:   `{"url": "http://localhost/api?key=val&foo=bar", "query": "a=1&b=2"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &domain.Request{Body: tt.body}
			out, err := exec.InterpolateRequest(req, tt.colEnv, tt.globalEnv)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.Body)
		})
	}
}

func TestInterpolateRequest_Headers(t *testing.T) {
	tests := []struct {
		name      string
		headers   string
		colEnv    map[string]string
		globalEnv map[string]string
		want      string
		wantErr   bool
	}{
		{
			name:    "header value substitution",
			headers: `{"Authorization": "Bearer {{token}}"}`,
			colEnv:  map[string]string{"token": "secret123"},
			want:    `{"Authorization":"Bearer secret123"}`,
		},
		{
			name:    "multiple header substitutions",
			headers: `{"Authorization": "Bearer {{token}}", "X-API-Key": "{{api_key}}"}`,
			colEnv:  map[string]string{"token": "abc", "api_key": "xyz"},
			want:    `{"Authorization":"Bearer abc","X-API-Key":"xyz"}`,
		},
		{
			name:    "no placeholders",
			headers: `{"Content-Type": "application/json"}`,
			colEnv:  map[string]string{"token": "ignored"},
			want:    `{"Content-Type": "application/json"}`,
		},
		{
			name:    "empty headers",
			headers: "",
			colEnv:  map[string]string{"token": "abc"},
			want:    "",
		},
		{
			name:    "empty object",
			headers: "{}",
			colEnv:  map[string]string{"token": "abc"},
			want:    "{}",
		},
		{
			name:    "unresolved in header",
			headers: `{"Authorization": "Bearer {{token}}"}`,
			colEnv:  map[string]string{"other": "value"},
			wantErr: true,
		},
		{
			name:    "malformed json left as-is",
			headers: `{"Authorization": "Bearer {{token}}"`, // missing closing brace
			colEnv:  map[string]string{"token": "abc"},
			want:    `{"Authorization": "Bearer {{token}}"`, // unchanged
		},
		{
			name:      "global fallback for header",
			headers:   `{"Authorization": "Bearer {{token}}"}`,
			globalEnv: map[string]string{"token": "global-token"},
			want:      `{"Authorization":"Bearer global-token"}`,
		},
		{
			name:      "collection overrides global for header",
			headers:   `{"Authorization": "Bearer {{token}}"}`,
			colEnv:    map[string]string{"token": "col-token"},
			globalEnv: map[string]string{"token": "global-token"},
			want:      `{"Authorization":"Bearer col-token"}`,
		},
		{
			name:    "empty header value",
			headers: `{"X-Custom": "{{empty}}"}`,
			colEnv:  map[string]string{"empty": ""},
			want:    `{"X-Custom":""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &domain.Request{Headers: tt.headers}
			out, err := exec.InterpolateRequest(req, tt.colEnv, tt.globalEnv)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.Headers)
		})
	}
}

func TestInterpolateRequest_AuthConfig(t *testing.T) {
	tests := []struct {
		name       string
		authConfig string
		colEnv     map[string]string
		globalEnv  map[string]string
		want       string
		wantErr    bool
	}{
		{
			name:       "bearer token substitution",
			authConfig: `{"token":"{{token}}"}`,
			colEnv:     map[string]string{"token": "secret123"},
			want:       `{"token":"secret123"}`,
		},
		{
			name:       "api key multi-field substitution",
			authConfig: `{"in":"header","name":"{{api_key_name}}","value":"{{api_key_value}}"}`,
			colEnv: map[string]string{
				"api_key_name":  "X-API-Key",
				"api_key_value": "xyz",
			},
			want: `{"in":"header","name":"X-API-Key","value":"xyz"}`,
		},
		{
			name:       "global fallback",
			authConfig: `{"token":"{{token}}"}`,
			globalEnv:  map[string]string{"token": "global-token"},
			want:       `{"token":"global-token"}`,
		},
		{
			name:       "unresolved auth config",
			authConfig: `{"token":"{{token}}"}`,
			colEnv:     map[string]string{"other": "value"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &domain.Request{AuthConfig: tt.authConfig}
			out, err := exec.InterpolateRequest(req, tt.colEnv, tt.globalEnv)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.AuthConfig)
		})
	}
}

func TestInterpolateRequest_FullRequest(t *testing.T) {
	req := &domain.Request{
		URL:        "https://{{host}}/api/{{version}}/users",
		Body:       `{"url": "{{base_url}}", "name": "{{name}}"}`,
		Headers:    `{"Authorization": "Bearer {{token}}", "Content-Type": "application/json"}`,
		AuthConfig: `{"token":"{{token}}"}`,
	}
	colEnv := map[string]string{
		"host":     "api.example.com",
		"version":  "v2",
		"base_url": "https://api.example.com",
		"token":    "col-secret",
	}
	globalEnv := map[string]string{
		"name":  "global-name",
		"token": "global-token",
	}

	out, err := exec.InterpolateRequest(req, colEnv, globalEnv)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com/api/v2/users", out.URL)
	assert.Equal(t, `{"url": "https://api.example.com", "name": "global-name"}`, out.Body)
	assert.Equal(
		t,
		`{"Authorization":"Bearer col-secret","Content-Type":"application/json"}`,
		out.Headers,
	)
	assert.Equal(t, `{"token":"col-secret"}`, out.AuthConfig)
}

func TestInterpolateRequestWithOverrides_PositionalAndFallbackSyntax(t *testing.T) {
	req := &domain.Request{
		URL:     "https://api.example.com/users/{{1|merchant_id}}",
		Body:    `{"merchant_id":"{{1|merchant_id}}","name":"{{name}}"}`,
		Headers: `{"Authorization":"Bearer {{token|api_token}}"}`,
	}

	out, err := exec.InterpolateRequestWithOverrides(
		req,
		[]string{"pos-123"},
		map[string]string{"name": "cli-name"},
		map[string]string{"merchant_id": "env-merchant", "token": "env-token"},
		map[string]string{"api_token": "global-token"},
	)
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com/users/pos-123", out.URL)
	assert.Equal(t, `{"merchant_id":"pos-123","name":"cli-name"}`, out.Body)
	assert.Equal(t, `{"Authorization":"Bearer env-token"}`, out.Headers)
}

func TestInterpolateRequestWithOverrides_NamedOverridesBeatEnv(t *testing.T) {
	req := &domain.Request{
		URL:  "https://{{host}}/api/{{merchant_id}}",
		Body: `{"merchant_id":"{{merchant_id}}"}`,
	}

	out, err := exec.InterpolateRequestWithOverrides(
		req,
		nil,
		map[string]string{"merchant_id": "cli-merchant"},
		map[string]string{"host": "example.com", "merchant_id": "env-merchant"},
		map[string]string{"merchant_id": "global-merchant"},
	)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/api/cli-merchant", out.URL)
	assert.Equal(t, `{"merchant_id":"cli-merchant"}`, out.Body)
}

func TestInterpolateRequestWithOverrides_FallbackUsesEnvWhenPositionalMissing(t *testing.T) {
	req := &domain.Request{
		URL: "https://{{ 1 | merchant_id }}/api",
	}

	out, err := exec.InterpolateRequestWithOverrides(
		req,
		nil,
		nil,
		map[string]string{"merchant_id": "merchant-from-env"},
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "https://merchant-from-env/api", out.URL)
}

func TestInterpolateRequestWithOverrides_FallbackErrorsWhenAllCandidatesMissing(t *testing.T) {
	req := &domain.Request{
		URL: "https://{{1|merchant_id}}/api",
	}

	_, err := exec.InterpolateRequestWithOverrides(req, nil, nil, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
	assert.Contains(t, err.Error(), "1|merchant_id")
}

func TestInterpolateRequest_ErrorsContainVariableName(t *testing.T) {
	req := &domain.Request{
		URL: "https://{{host}}/api",
	}
	_, err := exec.InterpolateRequest(req, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, exec.ErrUnresolvedVariable)
	assert.Contains(t, err.Error(), "host")
}

func TestInterpolateRequest_InputNotModified(t *testing.T) {
	req := &domain.Request{
		URL:  "https://{{host}}/api",
		Body: `{"url": "{{url}}"}`,
	}
	colEnv := map[string]string{"host": "example.com", "url": "http://localhost/"}

	out, err := exec.InterpolateRequest(req, colEnv, nil)
	require.NoError(t, err)

	// Original should be unchanged
	assert.Equal(t, "https://{{host}}/api", req.URL)
	assert.Equal(t, `{"url": "{{url}}"}`, req.Body)

	// Output should be substituted
	assert.Equal(t, "https://example.com/api", out.URL)
	assert.Equal(t, `{"url": "http://localhost/"}`, out.Body)
}

func TestInterpolateRequest_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		req       *domain.Request
		colEnv    map[string]string
		globalEnv map[string]string
		wantErr   bool
	}{
		{
			name: "all fields empty with envs",
			req: &domain.Request{
				URL:     "",
				Body:    "",
				Headers: "",
			},
			colEnv: map[string]string{"url": "http://localhost/"},
		},
		{
			name: "whitespace in variable name",
			req: &domain.Request{
				URL: "https://{{ host }}/api", // Postman doesn't use spaces, but we should handle it
			},
			colEnv:  map[string]string{"host": "example.com"},
			wantErr: true, // " host " != "host"
		},
		{
			name: "nested braces not matched",
			req: &domain.Request{
				URL: "https://{{host}/api", // missing closing }}
			},
			colEnv: map[string]string{"host": "example.com"},
		},
		{
			name: "special chars in value",
			req: &domain.Request{
				URL: "https://{{host}}/api",
			},
			colEnv: map[string]string{"host": "example.com:8080/path?query=1&foo=bar"},
		},
		{
			name: "unicode in value",
			req: &domain.Request{
				URL: "https://{{host}}/api",
			},
			colEnv: map[string]string{"host": "example.com/中文"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exec.InterpolateRequest(tt.req, tt.colEnv, tt.globalEnv)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestErrUnresolvedVariable_ErrorIs(t *testing.T) {
	// Verify ErrUnresolvedVariable is detectable with errors.Is
	err := fmt.Errorf("interpolate: %w: %q", exec.ErrUnresolvedVariable, "host")
	assert.True(t, errors.Is(err, exec.ErrUnresolvedVariable))
}
