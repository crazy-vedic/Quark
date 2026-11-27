package exec

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// collectionDefaultsFake serves collections by ID and no environments.
type collectionDefaultsFake struct {
	cols map[string]*domain.Collection
}

func (f collectionDefaultsFake) GetCollection(
	_ context.Context,
	id string,
) (*domain.Collection, error) {
	c, ok := f.cols[id]
	if !ok {
		return nil, errors.New("collection not found")
	}
	return c, nil
}

func (collectionDefaultsFake) GetEnvironment(context.Context, string) (*domain.Environment, error) {
	return nil, errors.New("unused")
}

func (collectionDefaultsFake) GetGlobalEnvironment(context.Context) (*domain.Environment, error) {
	return nil, errors.New("unused")
}

func (collectionDefaultsFake) GetActiveEnvironment(context.Context, string) (string, error) {
	return "", nil
}

func (collectionDefaultsFake) ListCollectionEnvironments(
	context.Context,
	string,
) ([]*domain.Environment, error) {
	return nil, nil
}

// defaultsTree builds root -> child, with the given auth and headers on each.
func defaultsTree(
	rootAuth domain.AuthConfig, rootAuthType string, rootHeaders string,
	childAuthType string, childHeaders string,
) collectionDefaultsFake {
	return collectionDefaultsFake{cols: map[string]*domain.Collection{
		"root": {
			ID: "root", Name: "root", AuthType: rootAuthType,
			AuthConfig: domain.MustAuthConfigJSON(rootAuth), Headers: rootHeaders,
		},
		"child": {
			ID: "child", Name: "child", ParentID: "root", AuthType: childAuthType,
			Headers: childHeaders,
		},
	}}
}

func inheritingRequest(headers string) *domain.Request {
	return &domain.Request{
		ID: "r", Method: http.MethodGet, URL: "https://example.test",
		CollectionID: "child", AuthType: domain.AuthTypeInherit, Headers: headers,
	}
}

func TestApplyCollectionDefaults_HeadersMergeRootToLeafAndRequestWins(t *testing.T) {
	f := defaultsTree(
		domain.AuthConfig{Token: "t"}, domain.AuthTypeNone,
		`{"X-Team":["core"],"X-Env":["prod"]}`,
		"", `{"X-Env":["staging"]}`,
	)
	out, err := ApplyCollectionDefaults(context.Background(), f,
		inheritingRequest(`{"x-team":"request"}`))
	require.NoError(t, err)

	headers, err := parseHeaders(out.Headers)
	require.NoError(t, err)
	assert.Equal(t, []string{"request"}, headers.Values("X-Team"),
		"the request replaces the collection value, whatever the case of the name")
	assert.Equal(t, []string{"staging"}, headers.Values("X-Env"),
		"the child replaces the parent value")
}

func TestApplyCollectionDefaults_AuthInheritsNearestSetting(t *testing.T) {
	ctx := context.Background()
	bearer := domain.AuthConfig{Token: "root-token"}

	t.Run("empty child defers to root", func(t *testing.T) {
		f := defaultsTree(bearer, domain.AuthTypeBearer, "", "", "")
		out, err := ApplyCollectionDefaults(ctx, f, inheritingRequest(""))
		require.NoError(t, err)
		assert.Equal(t, domain.AuthTypeBearer, out.AuthType)
		assert.JSONEq(t, `{"token":"root-token"}`, out.AuthConfig)
	})

	t.Run("child auth wins", func(t *testing.T) {
		f := defaultsTree(bearer, domain.AuthTypeBearer, "", domain.AuthTypeBasic, "")
		f.cols["child"].AuthConfig = `{"username":"u","password":"p"}`
		out, err := ApplyCollectionDefaults(ctx, f, inheritingRequest(""))
		require.NoError(t, err)
		assert.Equal(t, domain.AuthTypeBasic, out.AuthType)
		assert.JSONEq(t, `{"username":"u","password":"p"}`, out.AuthConfig)
	})

	t.Run("none stops inheritance", func(t *testing.T) {
		f := defaultsTree(bearer, domain.AuthTypeBearer, "", "none", "")
		out, err := ApplyCollectionDefaults(ctx, f, inheritingRequest(""))
		require.NoError(t, err)
		assert.Equal(t, domain.AuthTypeNone, out.AuthType)
		assert.Empty(t, out.AuthConfig)
	})

	t.Run("no collection sets auth", func(t *testing.T) {
		f := defaultsTree(bearer, "", "", "", "")
		out, err := ApplyCollectionDefaults(ctx, f, inheritingRequest(""))
		require.NoError(t, err)
		assert.Equal(t, domain.AuthTypeNone, out.AuthType)
	})

	t.Run("request with no auth never inherits", func(t *testing.T) {
		f := defaultsTree(bearer, domain.AuthTypeBearer, "", "", "")
		req := inheritingRequest("")
		req.AuthType = domain.AuthTypeNone
		out, err := ApplyCollectionDefaults(ctx, f, req)
		require.NoError(t, err)
		assert.Equal(t, domain.AuthTypeNone, out.AuthType)
	})

	t.Run("request Authorization header beats inherited auth", func(t *testing.T) {
		f := defaultsTree(bearer, domain.AuthTypeBearer, "", "", "")
		out, err := ApplyCollectionDefaults(ctx, f,
			inheritingRequest(`{"Authorization":"Custom abc"}`))
		require.NoError(t, err)
		assert.Equal(t, domain.AuthTypeNone, out.AuthType)
	})
}

func TestApplyCollectionDefaults_LeavesRequestWithoutDefaultsAlone(t *testing.T) {
	f := defaultsTree(domain.AuthConfig{}, "", "", "", "")
	req := inheritingRequest(`{"Accept":"application/json"}`)
	out, err := ApplyCollectionDefaults(context.Background(), f, req)
	require.NoError(t, err)
	assert.Equal(t, req.Headers, out.Headers, "stored headers are not rewritten")
	assert.Equal(t, domain.AuthTypeNone, out.AuthType)
	assert.NotSame(t, req, out, "the caller's request is never modified")
	assert.Equal(t, domain.AuthTypeInherit, req.AuthType)
}

func TestApplyCollectionDefaults_UnknownCollectionIsAHierarchyError(t *testing.T) {
	_, err := ApplyCollectionDefaults(context.Background(),
		collectionDefaultsFake{cols: map[string]*domain.Collection{}},
		inheritingRequest(""))
	require.ErrorIs(t, err, ErrCollectionHierarchy)
}

func TestApplyCollectionDefaults_InvalidCollectionHeadersFail(t *testing.T) {
	f := defaultsTree(domain.AuthConfig{}, "", "{", "", "")
	_, err := ApplyCollectionDefaults(context.Background(), f, inheritingRequest(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `collection "root" default headers`)
}

// The inherited token and header are sent, with the request's own header kept.
func TestExecute_SendsInheritedDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer root-token" ||
			r.Header.Get("X-Team") != "request" || r.Header.Get("X-Env") != "staging" {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	f := defaultsTree(
		domain.AuthConfig{Token: "root-token"}, domain.AuthTypeBearer,
		`{"X-Team":"core","X-Env":"prod"}`,
		"", `{"X-Env":"staging"}`,
	)
	req := inheritingRequest(`{"X-Team":"request"}`)
	req.URL = srv.URL

	prepared, err := ApplyCollectionDefaults(context.Background(), f, req)
	require.NoError(t, err)
	res, err := New(srv.Client().Transport).Execute(context.Background(), prepared)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}
