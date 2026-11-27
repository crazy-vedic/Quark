package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// IDs are not always UUIDs: imported and hand-seeded rows can be shorter than
// the eight characters the listing shows. Listing them must not panic.
func TestRequestList_ShortIDDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{ID: "col1", Name: "API"}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		ID: "r1", CollectionID: "col1", Name: "Short", Method: "GET", URL: "https://example.test",
	}))

	cmd := NewRequestCmd(st)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"list", "--collection", "API"})
	cmd.SetContext(ctx)

	require.NotPanics(t, func() { require.NoError(t, cmd.Execute()) })
	assert.Contains(t, out.String(), "r1")
	assert.Contains(t, out.String(), "Short")
}

// noResolverStore satisfies SearchStore but cannot resolve request paths.
type noResolverStore struct{ SearchStore }

type resolverStore struct {
	SearchStore
	req *domain.Request
}

func (s resolverStore) ResolveRequestPath(context.Context, string) (*domain.Request, error) {
	return s.req, nil
}

func TestRequestExportCurlNeedsAResolvingStore(t *testing.T) {
	cmd := newRequestExportCurlCmd(noResolverStore{})
	cmd.SetContext(context.Background())
	err := cmd.RunE(cmd, []string{"API/Anything"})
	require.ErrorContains(t, err, "cannot resolve request paths")
}

// resolverNoEnvStore resolves request paths but has no environments.
type resolverNoEnvStore struct{ resolverStore }

func TestRequestExportCurlNeedsEnvironments(t *testing.T) {
	cmd := newRequestExportCurlCmd(resolverNoEnvStore{resolverStore{req: &domain.Request{
		Method: "GET", URL: "https://example.test/x",
	}}})
	cmd.SetContext(context.Background())
	err := cmd.RunE(cmd, []string{"API/X"})
	require.ErrorContains(t, err, "cannot resolve environments")
}

func TestRequestExportCurlPrintsOneCommandPerRequest(t *testing.T) {
	ctx := context.Background()
	st, err := store.New(filepath.Join(t.TempDir(), "q.db"), store.WithCacheSize(10))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	col := &domain.Collection{ID: uuid.New().String(), Name: "API"}
	require.NoError(t, st.SaveCollection(ctx, col))
	env, err := st.CreateDefaultEnvironment(ctx, col.ID)
	require.NoError(t, err)
	env.SetVars(map[string]string{"host": "example.test"})
	require.NoError(t, st.SaveEnvironment(ctx, env))
	require.NoError(t, st.SetActiveEnvironment(ctx, col.ID, env.ID))
	req := &domain.Request{
		ID: uuid.New().String(), CollectionID: col.ID, Name: "X", Method: "GET",
		URL:     "https://{{host}}/x",
		Headers: `{"X-Multi":["a","b"]}`,
	}
	require.NoError(t, st.SaveRequest(ctx, req))

	cmd := newRequestExportCurlCmd(st)
	cmd.SetContext(ctx)
	var out bytes.Buffer
	cmd.SetOut(&out)

	require.NoError(t, cmd.RunE(cmd, []string{"API/X"}))
	require.Equal(t,
		"curl -X 'GET' -H 'X-Multi: a' -H 'X-Multi: b' 'https://example.test/x'\n",
		out.String())
}

// newRequestCRUDStore returns a real store with a root collection API that has
// a nested collection Users, and one request in each.
func newRequestCRUDStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.New(filepath.Join(t.TempDir(), "quark.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{ID: "col-api", Name: "API"}))
	require.NoError(t, st.SaveCollection(ctx, &domain.Collection{
		ID: "col-users", ParentID: "col-api", Name: "Users",
	}))
	require.NoError(t, st.SaveRequest(ctx, &domain.Request{
		ID: "req-health", CollectionID: "col-api", Name: "Health", Method: "GET",
		URL: "https://example.test/health", Enabled: true,
	}))
	return st
}

func runRequestCmd(t *testing.T, st SearchStore, args ...string) (string, error) {
	t.Helper()
	cmd := NewRequestCmd(st)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), err
}

func TestRequestCreateSavesAtAPath(t *testing.T) {
	st := newRequestCRUDStore(t)

	out, err := runRequestCmd(t, st, "create", "API/Users/List users",
		"--method", "post", "--url", "https://example.test/users")
	require.NoError(t, err)
	assert.Contains(t, out, `Created request "API/Users/List users"`)

	reqs, err := st.ListRequests(context.Background(), "col-users")
	require.NoError(t, err)
	require.Len(t, reqs, 1)
	assert.Equal(t, "List users", reqs[0].Name)
	assert.Equal(t, "POST", reqs[0].Method)
	assert.Equal(t, "https://example.test/users", reqs[0].URL)
	assert.True(t, reqs[0].Enabled)
	assert.Equal(t, "{}", reqs[0].Headers)
}

func TestRequestCreateDefaultsToGET(t *testing.T) {
	st := newRequestCRUDStore(t)

	_, err := runRequestCmd(t, st, "create", "API/Ping")
	require.NoError(t, err)

	req, err := lookupRequestPath(context.Background(), st, "API/Ping")
	require.NoError(t, err)
	assert.Equal(t, "GET", req.Method)
	assert.Empty(t, req.URL)
}

func TestRequestCreateRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no collection part",
			args:    []string{"create", "Ping"},
			wantErr: "must be 'Collection/Request'",
		},
		{
			name:    "empty request name",
			args:    []string{"create", "API/"},
			wantErr: "must be 'Collection/Request'",
		},
		{
			name:    "unknown collection",
			args:    []string{"create", "Missing/Ping"},
			wantErr: "resolve collection",
		},
		{
			name:    "bad method",
			args:    []string{"create", "API/Ping", "--method", "BREW"},
			wantErr: "--method must be one of",
		},
		{
			name:    "duplicate name",
			args:    []string{"create", "API/Health"},
			wantErr: "already exists",
		},
		{
			name:    "nested collection that does not exist",
			args:    []string{"create", "API/Users/Bad/Name", "--url", "x"},
			wantErr: `collection "API/Users/Bad"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newRequestCRUDStore(t)
			_, err := runRequestCmd(t, st, tt.args...)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRequestCreateNeedsAWritableStore(t *testing.T) {
	_, err := runRequestCmd(t, noResolverStore{}, "create", "API/Ping")
	require.ErrorContains(t, err, "store cannot save requests")
}

func TestRequestShowPrintsSavedFields(t *testing.T) {
	st := newRequestCRUDStore(t)

	out, err := runRequestCmd(t, st, "show", "API/Health")
	require.NoError(t, err)
	assert.Contains(t, out, "API/Health")
	assert.Contains(t, out, "GET")
	assert.Contains(t, out, "https://example.test/health")
	assert.Contains(t, out, "Enabled  yes")

	_, err = runRequestCmd(t, st, "show", "API/Nope")
	require.ErrorContains(t, err, `request "Nope" not found`)
}

func TestRequestRenameChangesOnlyTheName(t *testing.T) {
	st := newRequestCRUDStore(t)

	out, err := runRequestCmd(t, st, "rename", "API/Health", "Status")
	require.NoError(t, err)
	assert.Contains(t, out, `Renamed request "API/Health" to "Status"`)

	req, err := lookupRequestPath(context.Background(), st, "API/Status")
	require.NoError(t, err)
	assert.Equal(t, "req-health", req.ID)
	assert.Equal(t, "https://example.test/health", req.URL)
	assert.Equal(t, "GET", req.Method)
}

func TestRequestRenameRejectsClashesAndSlashes(t *testing.T) {
	st := newRequestCRUDStore(t)
	require.NoError(t, st.SaveRequest(context.Background(), &domain.Request{
		ID: "req-other", CollectionID: "col-api", Name: "Other", Method: "GET", Enabled: true,
	}))

	_, err := runRequestCmd(t, st, "rename", "API/Health", "Other")
	require.ErrorContains(t, err, "already exists")

	_, err = runRequestCmd(t, st, "rename", "API/Health", "A/B")
	require.ErrorContains(t, err, "must not contain '/'")

	_, err = runRequestCmd(t, st, "rename", "API/Health", "  ")
	require.ErrorContains(t, err, "must not be empty")
}

func TestRequestDeleteNeedsYes(t *testing.T) {
	st := newRequestCRUDStore(t)

	_, err := runRequestCmd(t, st, "delete", "API/Health")
	require.ErrorContains(t, err, "without --yes")
	_, err = lookupRequestPath(context.Background(), st, "API/Health")
	require.NoError(t, err, "request must survive a refused delete")

	out, err := runRequestCmd(t, st, "delete", "API/Health", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, `Deleted request "API/Health"`)
	_, err = lookupRequestPath(context.Background(), st, "API/Health")
	require.ErrorContains(t, err, "not found")
}
