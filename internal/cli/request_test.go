package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

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
