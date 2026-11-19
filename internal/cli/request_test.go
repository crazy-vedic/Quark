package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
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

func TestRequestExportCurlPrintsOneCommandPerRequest(t *testing.T) {
	st := resolverStore{req: &domain.Request{
		Method:  "GET",
		URL:     "https://example.test/x",
		Headers: `{"X-Multi":["a","b"]}`,
	}}
	cmd := newRequestExportCurlCmd(st)
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)

	require.NoError(t, cmd.RunE(cmd, []string{"API/X"}))
	require.Equal(t,
		"curl -X 'GET' -H 'X-Multi: a' -H 'X-Multi: b' 'https://example.test/x'\n",
		out.String())
}
