package exec_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

func newExtractStore(t *testing.T) (*store.Store, *domain.Collection) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "q.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	col := &domain.Collection{Name: "api"}
	require.NoError(t, st.SaveCollection(context.Background(), col))
	_, err = st.CreateDefaultEnvironment(context.Background(), col.ID)
	if err != nil {
		// Some store versions create the default environment on SaveCollection.
		require.ErrorIs(t, err, store.ErrDuplicate)
	}
	return st, col
}

func extractServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "rid-7")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"data":{"token":"tok-1","n":5}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func envVars(t *testing.T, st *store.Store, colID string) map[string]string {
	t.Helper()
	ctx := context.Background()
	activeID, err := st.GetActiveEnvironment(ctx, colID)
	require.NoError(t, err)
	if activeID != "" {
		env, err := st.GetEnvironment(ctx, activeID)
		require.NoError(t, err)
		return env.Vars()
	}
	envs, err := st.ListEnvironments(ctx, colID)
	require.NoError(t, err)
	for _, env := range envs {
		if env.Name == "default" {
			return env.Vars()
		}
	}
	t.Fatal("no default environment")
	return nil
}

func TestExtractionHook_WritesDefaultEnvironment(t *testing.T) {
	st, col := newExtractStore(t)
	srv := extractServer(t, http.StatusOK)
	e := exec.New(http.DefaultTransport,
		exec.WithPostResponseHooks([]exec.PostResponseHook{exec.NewExtractionHook(st)}))

	req := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: srv.URL,
		Extractions: domain.EncodeExtractions([]domain.Extraction{
			{Variable: "token", Source: domain.ExtractFromBody, Path: "$.data.token"},
			{Variable: "n", Source: domain.ExtractFromBody, Path: "data.n"},
			{Variable: "rid", Source: domain.ExtractFromHeader, Path: "x-request-id"},
			{Variable: "missing", Source: domain.ExtractFromBody, Path: "$.nope"},
		}),
	}
	res, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, res.Extracted, 4)
	assert.NoError(t, res.Extracted[0].Err)
	assert.Equal(t, "default", res.Extracted[0].Environment)
	assert.Error(t, res.Extracted[3].Err)

	assert.Equal(t, map[string]string{"token": "tok-1", "n": "5", "rid": "rid-7"},
		envVars(t, st, col.ID))
}

func TestExtractionHook_WritesActiveEnvironment(t *testing.T) {
	st, col := newExtractStore(t)
	ctx := context.Background()
	dev := &domain.Environment{CollectionID: col.ID, Name: "dev", Data: `{"keep":"1"}`}
	require.NoError(t, st.SaveEnvironment(ctx, dev))
	require.NoError(t, st.SetActiveEnvironment(ctx, col.ID, dev.ID))
	srv := extractServer(t, http.StatusCreated)

	hook := exec.NewExtractionHook(st)
	e := exec.New(http.DefaultTransport,
		exec.WithPostResponseHooks([]exec.PostResponseHook{hook}))
	req := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: srv.URL,
		Extractions: `[{"variable":"token","source":"body","path":"$.data.token"}]`,
	}
	res, err := e.Execute(ctx, req)
	require.NoError(t, err)
	require.Len(t, res.Extracted, 1)
	assert.Equal(t, "dev", res.Extracted[0].Environment)
	assert.Equal(t, map[string]string{"keep": "1", "token": "tok-1"}, envVars(t, st, col.ID))
}

func TestExtractionHook_SkipsNon2xx(t *testing.T) {
	st, col := newExtractStore(t)
	srv := extractServer(t, http.StatusUnauthorized)
	e := exec.New(http.DefaultTransport,
		exec.WithPostResponseHooks([]exec.PostResponseHook{exec.NewExtractionHook(st)}))
	req := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: srv.URL,
		Extractions: `[{"variable":"token","source":"body","path":"$.data.token"}]`,
	}
	res, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, res.Extracted, 1)
	assert.ErrorIs(t, res.Extracted[0].Err, exec.ErrExtractionSkipped)
	assert.Empty(t, envVars(t, st, col.ID))
}

func TestExtractionHook_GlobalForUnsavedRequest(t *testing.T) {
	st, _ := newExtractStore(t)
	hook := exec.NewExtractionHook(st)
	res := &exec.ExecuteResult{
		StatusCode: 200,
		Headers:    map[string][]string{"X-A": {"1", "2"}},
		Body:       []byte("not json"),
	}
	req := &domain.Request{Extractions: `[
		{"variable":"a","source":"header","path":"X-A"},
		{"variable":"b","source":"body","path":"$.x"},
		{"variable":"c","source":"header","path":"X-None"},
		{"variable":"d","source":"bogus","path":"x"}]`}
	require.NoError(t, hook.AfterResponse(context.Background(), req, res))
	require.Len(t, res.Extracted, 4)
	assert.Equal(t, "global", res.Extracted[0].Environment)
	assert.Error(t, res.Extracted[1].Err)
	assert.Error(t, res.Extracted[2].Err)
	assert.Error(t, res.Extracted[3].Err)

	global, err := st.GetGlobalEnvironment(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "1, 2"}, global.Vars())
}

func TestExtractionHook_NoRulesAndBadRules(t *testing.T) {
	st, _ := newExtractStore(t)
	hook := exec.NewExtractionHook(st)
	res := &exec.ExecuteResult{StatusCode: 200}
	require.NoError(t, hook.AfterResponse(context.Background(), &domain.Request{}, res))
	assert.Empty(t, res.Extracted)
	require.NoError(
		t,
		hook.AfterResponse(context.Background(), &domain.Request{Extractions: "["}, res),
	)
	require.Len(t, res.Extracted, 1)
	assert.Error(t, res.Extracted[0].Err)
	require.NoError(t, hook.AfterResponse(context.Background(), nil, res))
}

func TestExtractionHook_StreamedBody(t *testing.T) {
	st, col := newExtractStore(t)
	srv := extractServer(t, http.StatusOK)
	e := exec.New(http.DefaultTransport,
		exec.WithMaxResponseSize(4),
		exec.WithPostResponseHooks([]exec.PostResponseHook{exec.NewExtractionHook(st)}))
	req := &domain.Request{
		CollectionID: col.ID, Method: "GET", URL: srv.URL,
		Extractions: `[{"variable":"token","source":"body","path":"$.data.token"}]`,
	}
	res, err := e.Execute(context.Background(), req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Cleanup() })
	require.True(t, res.IsStreamed())
	require.Len(t, res.Extracted, 1)
	require.NoError(t, res.Extracted[0].Err)
	assert.Equal(t, "tok-1", envVars(t, st, col.ID)["token"])
}

func TestInterpolateRequestKeepsExtractions(t *testing.T) {
	req := &domain.Request{URL: "http://{{h}}", Extractions: `[{"variable":"a"}]`}
	out, err := exec.InterpolateRequest(req, map[string]string{"h": "x"}, nil)
	require.NoError(t, err)
	assert.Equal(t, req.Extractions, out.Extractions)
}
