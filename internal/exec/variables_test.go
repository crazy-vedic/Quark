package exec_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

type hierarchyResolver struct {
	collections   map[string]*domain.Collection
	environments  map[string]*domain.Environment
	byCollection  map[string][]*domain.Environment
	global        *domain.Environment
	collectionErr map[string]error
	listErr       map[string]error
}

func (r *hierarchyResolver) GetCollection(_ context.Context, id string) (*domain.Collection, error) {
	if err := r.collectionErr[id]; err != nil {
		return nil, err
	}
	collection, ok := r.collections[id]
	if !ok {
		return nil, errors.New("collection missing")
	}
	return collection, nil
}

func (r *hierarchyResolver) GetEnvironment(_ context.Context, id string) (*domain.Environment, error) {
	environment, ok := r.environments[id]
	if !ok {
		return nil, errors.New("environment missing")
	}
	return environment, nil
}

func (r *hierarchyResolver) GetGlobalEnvironment(context.Context) (*domain.Environment, error) {
	if r.global == nil {
		return nil, errors.New("Global missing")
	}
	return r.global, nil
}

func (r *hierarchyResolver) ListCollectionEnvironments(_ context.Context, id string) ([]*domain.Environment, error) {
	if err := r.listErr[id]; err != nil {
		return nil, err
	}
	return r.byCollection[id], nil
}

func testEnvironment(id, collectionID, name string, vars map[string]string) *domain.Environment {
	environment := &domain.Environment{ID: id, CollectionID: collectionID, Name: name}
	environment.SetVars(vars)
	return environment
}

func newHierarchyResolver() *hierarchyResolver {
	return &hierarchyResolver{
		collections: make(map[string]*domain.Collection), environments: make(map[string]*domain.Environment),
		byCollection: make(map[string][]*domain.Environment), collectionErr: make(map[string]error), listErr: make(map[string]error),
		global: testEnvironment("global", "", "global", map[string]string{"shared": "global", "global-only": "yes"}),
	}
}

func (r *hierarchyResolver) addCollection(id, parentID string, environments ...*domain.Environment) {
	r.collections[id] = &domain.Collection{ID: id, ParentID: parentID, Name: id}
	r.byCollection[id] = environments
	for _, environment := range environments {
		r.environments[environment.ID] = environment
	}
}

func TestResolveEnvVars_HierarchyPrecedenceAndChildActiveName(t *testing.T) {
	r := newHierarchyResolver()
	r.addCollection("root", "",
		testEnvironment("root-default", "root", "default", map[string]string{"shared": "root-default", "root-default": "yes"}),
		testEnvironment("root-dev", "root", "dev", map[string]string{"shared": "root-dev", "root-dev": "yes"}),
		testEnvironment("root-prod", "root", "prod", map[string]string{"ignored": "no"}),
	)
	r.addCollection("parent", "root",
		testEnvironment("parent-default", "parent", "default", map[string]string{"shared": "parent-default"}),
		testEnvironment("parent-dev", "parent", "dev", map[string]string{"shared": "parent-dev"}),
	)
	r.addCollection("child", "parent",
		testEnvironment("child-default", "child", "default", map[string]string{"shared": "child-default", "empty": "base"}),
		testEnvironment("child-dev", "child", "dev", map[string]string{"shared": "child-dev", "empty": ""}),
	)

	collectionVars, globalVars, err := exec.ResolveEnvVars(context.Background(), r, "child-dev", "child")
	require.NoError(t, err)
	assert.Equal(t, "child-dev", collectionVars["shared"])
	assert.Equal(t, "", collectionVars["empty"])
	assert.Equal(t, "yes", collectionVars["root-default"])
	assert.Equal(t, "yes", collectionVars["root-dev"])
	assert.NotContains(t, collectionVars, "ignored")
	assert.Equal(t, "global", globalVars["shared"])

	collectionVars["shared"] = "mutated"
	again, _, err := exec.ResolveEnvVars(context.Background(), r, "child-dev", "child")
	require.NoError(t, err)
	assert.Equal(t, "child-dev", again["shared"], "resolved maps must not alias stored state")
}

func TestResolveEnvVars_AllSharedKeyPresenceCombinations(t *testing.T) {
	for mask := 0; mask < 128; mask++ {
		t.Run(fmt.Sprintf("mask_%03d", mask), func(t *testing.T) {
			r := newHierarchyResolver()
			if mask&1 == 0 {
				r.global.SetVars(map[string]string{})
			}
			layers := make([]*domain.Environment, 0, 6)
			for i, spec := range []struct{ id, owner, name string }{
				{"root-default", "root", "default"}, {"root-active", "root", "dev"},
				{"parent-default", "parent", "default"}, {"parent-active", "parent", "dev"},
				{"child-default", "child", "default"}, {"child-active", "child", "dev"},
			} {
				vars := map[string]string{}
				if mask&(1<<(i+1)) != 0 {
					vars["shared"] = spec.id
				}
				layers = append(layers, testEnvironment(spec.id, spec.owner, spec.name, vars))
			}
			r.addCollection("root", "", layers[0], layers[1])
			r.addCollection("parent", "root", layers[2], layers[3])
			r.addCollection("child", "parent", layers[4], layers[5])
			vars, global, err := exec.ResolveEnvVars(context.Background(), r, "child-active", "child")
			require.NoError(t, err)
			want, present := "", false
			if mask&1 != 0 {
				want, present = "global", true
			}
			for i, layer := range layers {
				if mask&(1<<(i+1)) != 0 {
					want, present = layer.ID, true
				}
			}
			got, collectionPresent := vars["shared"]
			switch {
			case collectionPresent:
				assert.Equal(t, want, got)
			case present:
				assert.Equal(t, want, global["shared"])
			default:
				assert.NotContains(t, global, "shared")
			}
		})
	}
}

func TestResolveEnvVars_DeepHierarchy(t *testing.T) {
	r := newHierarchyResolver()
	parent := ""
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("level-%03d", i)
		r.addCollection(id, parent, testEnvironment("default-"+id, id, "default", map[string]string{"depth": fmt.Sprint(i)}))
		parent = id
	}
	vars, _, err := exec.ResolveEnvVars(context.Background(), r, "", parent)
	require.NoError(t, err)
	assert.Equal(t, "99", vars["depth"])
}

func TestResolveEnvVars_Failures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*hierarchyResolver)
		active string
		want   error
	}{
		{name: "missing default", mutate: func(r *hierarchyResolver) { r.byCollection["child"] = nil }, want: exec.ErrMissingDefaultEnvironment},
		{name: "self cycle", mutate: func(r *hierarchyResolver) { r.collections["child"].ParentID = "child" }, want: exec.ErrCollectionHierarchy},
		{name: "invalid data", mutate: func(r *hierarchyResolver) { r.byCollection["child"][0].Data = `{"bad":1}` }, want: domain.ErrInvalidEnvironmentData},
		{name: "sibling active", mutate: func(r *hierarchyResolver) {
			sibling := testEnvironment("sibling-dev", "sibling", "dev", map[string]string{})
			r.environments[sibling.ID] = sibling
		}, active: "sibling-dev", want: exec.ErrEnvironmentOwnership},
		{name: "list failure", mutate: func(r *hierarchyResolver) { r.listErr["child"] = errors.New("read failed") }, want: exec.ErrEnvironmentResolution},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newHierarchyResolver()
			r.addCollection("child", "", testEnvironment("child-default", "child", "default", map[string]string{}))
			tt.mutate(r)
			_, _, err := exec.ResolveEnvVars(context.Background(), r, tt.active, "child")
			assert.ErrorIs(t, err, tt.want)
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := newHierarchyResolver()
	r.addCollection("child", "", testEnvironment("child-default", "child", "default", map[string]string{}))
	_, _, err := exec.ResolveEnvVars(ctx, r, "", "child")
	assert.ErrorIs(t, err, context.Canceled)
}

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
