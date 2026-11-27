package runner

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

type fixture struct {
	st  *store.Store
	ex  *exec.Executor
	api *domain.Collection
	ctx context.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "runner.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	api := &domain.Collection{Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, api))
	if _, err := st.CreateDefaultEnvironment(ctx, api.ID); err != nil {
		require.ErrorIs(t, err, store.ErrDuplicate)
	}
	ex := exec.New(http.DefaultTransport, exec.WithPostResponseHooks([]exec.PostResponseHook{
		exec.NewExtractionHook(st),
		exec.NewAssertionHook(),
	}))
	return fixture{st: st, ex: ex, api: api, ctx: ctx}
}

func (f fixture) save(t *testing.T, col *domain.Collection, req *domain.Request) {
	t.Helper()
	req.CollectionID = col.ID
	require.NoError(t, f.st.SaveRequest(f.ctx, req))
}

func names(results []Result) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Name)
	}
	return out
}

func TestRunOrdersRequestsThenChildCollections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	f := newFixture(t)

	f.save(t, f.api, &domain.Request{Name: "second", Method: "GET", URL: srv.URL, SortOrder: 2})
	f.save(t, f.api, &domain.Request{Name: "first", Method: "GET", URL: srv.URL, SortOrder: 1})
	users := &domain.Collection{Name: "Users", ParentID: f.api.ID}
	require.NoError(t, f.st.SaveCollection(f.ctx, users))
	f.save(t, users, &domain.Request{Name: "list", Method: "GET", URL: srv.URL})

	results, err := Run(f.ctx, f.st, f.ex, "API", Options{})
	require.NoError(t, err)
	assert.Equal(t, []string{"API/first", "API/second", "API/Users/list"}, names(results))
	assert.Equal(t, 0, Failed(results))
	assert.Equal(t, "API", results[0].Collection)
	assert.Equal(t, "API/Users", results[2].Collection)
	assert.Equal(t, 200, results[0].StatusCode)
}

func TestRunReportsAssertionFailuresAndTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	gone := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	goneURL := gone.URL
	gone.Close()
	defer srv.Close()
	f := newFixture(t)

	f.save(t, f.api, &domain.Request{
		Name: "checked", Method: "GET", URL: srv.URL, SortOrder: 1,
		Assertions: domain.EncodeAssertions([]domain.Assertion{
			{Kind: domain.AssertStatus, Op: "=", Value: "200"},
		}),
	})
	f.save(
		t,
		f.api,
		&domain.Request{Name: "unreachable", Method: "GET", URL: goneURL, SortOrder: 2},
	)

	results, err := Run(f.ctx, f.st, f.ex, "API", Options{})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, Fail, results[0].Status)
	assert.Equal(t, []string{"status = 200: status is 500"}, results[0].Messages)
	assert.Equal(t, Fail, results[1].Status)
	require.Len(t, results[1].Messages, 1)
	assert.Contains(t, results[1].Messages[0], "request failed")
	assert.Equal(t, 2, Failed(results))
}

func TestRunBailStopsAfterFirstFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	f := newFixture(t)
	f.save(t, f.api, &domain.Request{
		Name: "a", Method: "GET", URL: srv.URL, SortOrder: 1,
		Assertions: domain.EncodeAssertions([]domain.Assertion{
			{Kind: domain.AssertStatus, Op: "=", Value: "200"},
		}),
	})
	f.save(t, f.api, &domain.Request{Name: "b", Method: "GET", URL: srv.URL, SortOrder: 2})

	results, err := Run(f.ctx, f.st, f.ex, "API", Options{Bail: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"API/a"}, names(results))
	assert.Equal(t, 1, Failed(results))
}

func TestRunCarriesExtractedValuesToLaterRequests(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = w.Write([]byte(`{"token":"tok-9"}`))
			return
		}
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()
	f := newFixture(t)

	f.save(t, f.api, &domain.Request{
		Name: "login", Method: "POST", URL: srv.URL + "/login", SortOrder: 1,
		Extractions: domain.EncodeExtractions([]domain.Extraction{
			{Variable: "token", Source: domain.ExtractFromBody, Path: "$.token"},
		}),
		Assertions: domain.EncodeAssertions([]domain.Assertion{
			{Kind: domain.AssertJSONPath, Target: "$.token", Op: "exists"},
		}),
	})
	f.save(t, f.api, &domain.Request{
		Name: "me", Method: "GET", URL: srv.URL + "/me", SortOrder: 2,
		Headers: `{"Authorization":"Bearer {{token}}"}`,
	})

	results, err := Run(f.ctx, f.st, f.ex, "API", Options{})
	require.NoError(t, err)
	assert.Equal(t, 0, Failed(results))
	assert.Equal(t, "Bearer tok-9", gotAuth)
}

func TestRunUnknownCollection(t *testing.T) {
	f := newFixture(t)
	_, err := Run(f.ctx, f.st, f.ex, "Nope", Options{})
	assert.Error(t, err)
}
