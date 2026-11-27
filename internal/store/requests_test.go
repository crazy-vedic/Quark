package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

func saveOptionsTestRequest(
	t *testing.T,
	s *store.Store,
	opts domain.RequestOptions,
) *domain.Request {
	t.Helper()
	ctx := context.Background()
	col := &domain.Collection{ID: uuid.New().String(), Name: "Options"}
	require.NoError(t, s.SaveCollection(ctx, col))
	req := &domain.Request{
		CollectionID: col.ID,
		Name:         "Slow",
		Method:       "GET",
		URL:          "https://example.com/slow",
		Headers:      "{}",
		Enabled:      true,
		Options:      opts,
	}
	require.NoError(t, s.SaveRequest(ctx, req))
	return req
}

func TestRequestOptions_PersistAndReloadThroughEveryReadPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	follow := false
	maxRedirects := 2
	want := domain.RequestOptions{
		TimeoutMS:       1500,
		FollowRedirects: &follow,
		MaxRedirects:    &maxRedirects,
		Proxy:           "http://proxy.internal:3128",
		SkipTLSVerify:   true,
	}
	req := saveOptionsTestRequest(t, s, want)

	got, err := s.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.Equal(t, want.TimeoutMS, got.Options.TimeoutMS)
	require.NotNil(t, got.Options.FollowRedirects)
	assert.False(t, *got.Options.FollowRedirects)
	require.NotNil(t, got.Options.MaxRedirects)
	assert.Equal(t, 2, *got.Options.MaxRedirects)
	assert.Equal(t, want.Proxy, got.Options.Proxy)
	assert.True(t, got.Options.SkipTLSVerify)

	listed, err := s.ListRequests(ctx, req.CollectionID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, want.Proxy, listed[0].Options.Proxy, "list must load options too")

	// quark run resolves requests by path, which has its own query.
	resolved, err := s.ResolveRequestPath(ctx, "Slow")
	require.NoError(t, err)
	assert.Equal(t, want.TimeoutMS, resolved.Options.TimeoutMS,
		"path resolution must load options too, or quark run would drop them")
	assert.True(t, resolved.Options.SkipTLSVerify)
}

func TestRequestOptions_DefaultRequestStoresEmptyObject(t *testing.T) {
	s := newTestStore(t)
	req := saveOptionsTestRequest(t, s, domain.RequestOptions{})

	got, err := s.GetRequest(context.Background(), req.ID)
	require.NoError(t, err)
	assert.True(t, got.Options.IsZero())
}

func TestRequestOptions_UpdateClearsOptions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	req := saveOptionsTestRequest(t, s, domain.RequestOptions{TimeoutMS: 900})

	req.Options = domain.RequestOptions{}
	require.NoError(t, s.SaveRequest(ctx, req))

	got, err := s.GetRequest(ctx, req.ID)
	require.NoError(t, err)
	assert.True(t, got.Options.IsZero())
}

func TestRequestOptions_InvalidOptionsAreNotSaved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	col := &domain.Collection{ID: uuid.New().String(), Name: "Bad"}
	require.NoError(t, s.SaveCollection(ctx, col))
	req := &domain.Request{
		CollectionID: col.ID,
		Name:         "Bad proxy",
		Method:       "GET",
		URL:          "https://example.com",
		Options:      domain.RequestOptions{Proxy: "ftp://files.example"},
	}

	err := s.SaveRequest(ctx, req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidRequestOptions))

	listed, err := s.ListRequests(ctx, col.ID)
	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestRequestOptions_CorruptStoredOptionsFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	s, err := store.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	req := saveOptionsTestRequest(t, s, domain.RequestOptions{TimeoutMS: 900})

	_, err = s.DB().Exec(
		`UPDATE requests SET options = '{"no_such_option":true}' WHERE id = ?`, req.ID,
	)
	require.NoError(t, err)

	_, err = s.GetRequest(context.Background(), req.ID)
	require.Error(t, err, "options that cannot be applied must not be silently dropped")
	assert.True(t, errors.Is(err, domain.ErrInvalidRequestOptions))
}
