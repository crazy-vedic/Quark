package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
)

func TestParseRunOverrides(t *testing.T) {
	positionals, named, err := parseRunOverrides(
		[]string{"123", "abc"},
		[]string{"merchant_id=mid", "token=secret"},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"123", "abc"}, positionals)
	assert.Equal(t, map[string]string{
		"merchant_id": "mid",
		"token":       "secret",
	}, named)
}

func TestParseRunOverrides_RejectsInvalidAssignments(t *testing.T) {
	_, _, err := parseRunOverrides(nil, []string{"merchant_id"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected key=value")

	_, _, err = parseRunOverrides(nil, []string{" =value"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key cannot be empty")
}

type fakeRunStore struct {
	collections []*domain.Collection
	requests    map[string][]*domain.Request
	globalEnv   *domain.Environment
	envsByID    map[string]*domain.Environment
	envsByCol   map[string][]*domain.Environment
	activeEnvID map[string]string
}

func (s *fakeRunStore) ListCollections(context.Context) ([]*domain.Collection, error) {
	return s.collections, nil
}

func (s *fakeRunStore) GetRequest(_ context.Context, id string) (*domain.Request, error) {
	for _, reqs := range s.requests {
		for _, req := range reqs {
			if req.ID == id {
				return req, nil
			}
		}
	}
	return nil, nil
}

func (s *fakeRunStore) ListRequests(
	_ context.Context,
	collectionID string,
) ([]*domain.Request, error) {
	return s.requests[collectionID], nil
}

func (s *fakeRunStore) GetEnvironment(_ context.Context, id string) (*domain.Environment, error) {
	if env, ok := s.envsByID[id]; ok {
		return env, nil
	}
	return nil, nil
}

func (s *fakeRunStore) GetGlobalEnvironment(context.Context) (*domain.Environment, error) {
	return s.globalEnv, nil
}

func (s *fakeRunStore) ListEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	if collectionID == "" {
		if s.globalEnv == nil {
			return nil, nil
		}
		return []*domain.Environment{s.globalEnv}, nil
	}
	return s.envsByCol[collectionID], nil
}

func (s *fakeRunStore) ListCollectionEnvironments(
	_ context.Context,
	collectionID string,
) ([]*domain.Environment, error) {
	return s.envsByCol[collectionID], nil
}

func (s *fakeRunStore) ListAllEnvironments(context.Context) ([]*domain.Environment, error) {
	var all []*domain.Environment
	for _, envs := range s.envsByCol {
		all = append(all, envs...)
	}
	return all, nil
}

func (s *fakeRunStore) GetActiveEnvironment(
	_ context.Context,
	collectionID string,
) (string, error) {
	return s.activeEnvID[collectionID], nil
}

func (s *fakeRunStore) SetActiveEnvironment(_ context.Context, collectionID, envID string) error {
	if s.activeEnvID == nil {
		s.activeEnvID = make(map[string]string)
	}
	s.activeEnvID[collectionID] = envID
	return nil
}

type recordingRoundTripper struct {
	lastMethod  string
	lastURL     string
	lastBody    string
	lastHeaders http.Header
	response    *http.Response
}

func (rt *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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

func TestNewRunCmd_ExecutesWithPositionalsNamedVarsAndEnvFallback(t *testing.T) {
	const collectionID = "col-1"

	colEnv := &domain.Environment{ID: "env-1", CollectionID: collectionID, Name: "default"}
	colEnv.SetVars(map[string]string{
		"merchant_id": "env-merchant",
		"token":       "env-token",
	})
	globalEnv := &domain.Environment{ID: "global", Name: "Global"}
	globalEnv.SetVars(map[string]string{
		"name": "global-name",
	})

	st := &fakeRunStore{
		collections: []*domain.Collection{{ID: collectionID, Name: "API"}},
		requests: map[string][]*domain.Request{
			collectionID: {{
				ID:           "req-1",
				CollectionID: collectionID,
				Name:         "create-item",
				Method:       "POST",
				URL:          "https://example.test/{{1|merchant_id}}",
				Body:         `{"merchant_id":"{{1|merchant_id}}","name":"{{name}}"}`,
				Headers:      `{"Authorization":"Bearer {{token}}"}`,
			}},
		},
		globalEnv: globalEnv,
		envsByID:  map[string]*domain.Environment{"env-1": colEnv},
		envsByCol: map[string][]*domain.Environment{collectionID: {colEnv}},
		activeEnvID: map[string]string{
			collectionID: "env-1",
		},
	}

	transport := &recordingRoundTripper{
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
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, "POST", transport.lastMethod)
	assert.Equal(t, "https://example.test/pos-123", transport.lastURL)
	assert.Equal(t, `{"merchant_id":"pos-123","name":"cli-name"}`, transport.lastBody)
	assert.Equal(t, "Bearer cli-token", transport.lastHeaders.Get("Authorization"))
	assert.Contains(t, out.String(), "Status: 200 OK")
	assert.Contains(t, out.String(), `{"ok":true}`)
}

func TestNewRunCmd_FallbackUsesEnvWhenPositionalMissing(t *testing.T) {
	const collectionID = "col-1"

	colEnv := &domain.Environment{ID: "env-1", CollectionID: collectionID, Name: "default"}
	colEnv.SetVars(map[string]string{"merchant_id": "env-merchant"})

	st := &fakeRunStore{
		collections: []*domain.Collection{{ID: collectionID, Name: "API"}},
		requests: map[string][]*domain.Request{
			collectionID: {{
				ID:           "req-1",
				CollectionID: collectionID,
				Name:         "create-item",
				Method:       "GET",
				URL:          "https://example.test/{{1|merchant_id}}",
			}},
		},
		globalEnv:   &domain.Environment{ID: "global", Name: "Global"},
		envsByID:    map[string]*domain.Environment{"env-1": colEnv},
		envsByCol:   map[string][]*domain.Environment{collectionID: {colEnv}},
		activeEnvID: map[string]string{},
	}
	st.globalEnv.SetVars(map[string]string{})

	transport := &recordingRoundTripper{
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
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, "https://example.test/env-merchant", transport.lastURL)
}

func TestNewRunCmd_ErrorsWhenFallbackCandidatesUnresolved(t *testing.T) {
	const collectionID = "col-1"

	st := &fakeRunStore{
		collections: []*domain.Collection{{ID: collectionID, Name: "API"}},
		requests: map[string][]*domain.Request{
			collectionID: {{
				ID:           "req-1",
				CollectionID: collectionID,
				Name:         "create-item",
				Method:       "GET",
				URL:          "https://example.test/{{1|merchant_id}}",
			}},
		},
		globalEnv:   &domain.Environment{ID: "global", Name: "Global"},
		envsByID:    map[string]*domain.Environment{},
		envsByCol:   map[string][]*domain.Environment{},
		activeEnvID: map[string]string{},
	}
	st.globalEnv.SetVars(map[string]string{})

	transport := &recordingRoundTripper{
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
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `1|merchant_id`)
}

func TestNewRunCmd_ExecutesStructuredAuth(t *testing.T) {
	const collectionID = "col-1"

	st := &fakeRunStore{
		collections: []*domain.Collection{{ID: collectionID, Name: "API"}},
		requests: map[string][]*domain.Request{
			collectionID: {{
				ID:           "req-1",
				CollectionID: collectionID,
				Name:         "create-item",
				Method:       "GET",
				URL:          "https://example.test/users",
				AuthType:     domain.AuthTypeAPIKey,
				AuthConfig:   `{"in":"header","name":"X-API-Key","value":"{{api_key}}"}`,
			}},
		},
		globalEnv: &domain.Environment{ID: "global", Name: "Global"},
	}
	st.globalEnv.SetVars(map[string]string{"api_key": "secret"})

	transport := &recordingRoundTripper{
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
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Equal(t, "secret", transport.lastHeaders.Get("X-API-Key"))
}
