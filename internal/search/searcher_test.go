package search_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/search/mocks"
)

const testCollectionID = "col-001"

func stubRequests(ctrl *gomock.Controller, reqs []*domain.Request) *mocks.MockRequestReader {
	mock := mocks.NewMockRequestReader(ctrl)
	mock.EXPECT().
		ListRequests(gomock.Any(), testCollectionID).
		Return(reqs, nil).
		AnyTimes()
	return mock
}

func TestSearcher_MatchByName(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "List Users", Method: "GET", URL: "http://api/users"},
		{ID: "r2", Name: "Create Payment", Method: "POST", URL: "http://api/payments"},
		{ID: "r3", Name: "Get Order", Method: "GET", URL: "http://api/orders"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "users")
	require.NoError(t, err)
	require.NotEmpty(t, result.Hits)
	assert.Equal(t, "r1", result.Hits[0].Request.ID)
}

func TestSearcher_MatchByURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "Alpha", Method: "GET", URL: "http://api/v1/users"},
		{ID: "r2", Name: "Beta", Method: "GET", URL: "http://api/v2/payments"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "v1")
	require.NoError(t, err)
	require.NotEmpty(t, result.Hits)
	assert.Equal(t, "r1", result.Hits[0].Request.ID)
}

func TestSearcher_EmptyQuery_ReturnsAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "A", Method: "GET", URL: "http://api/a"},
		{ID: "r2", Name: "B", Method: "GET", URL: "http://api/b"},
		{ID: "r3", Name: "C", Method: "GET", URL: "http://api/c"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "")
	require.NoError(t, err)
	assert.Len(t, result.Hits, 3)
}

func TestSearcher_NoMatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "List Users", Method: "GET", URL: "http://api/users"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "zzznomatch")
	require.NoError(t, err)
	assert.Empty(t, result.Hits)
}

func TestSearcher_SearchAll_MergesHitsAcrossCollections(t *testing.T) {
	ctrl := gomock.NewController(t)
	mock := mocks.NewMockRequestReader(ctrl)
	mock.EXPECT().
		ListRequests(gomock.Any(), "col-1").
		Return([]*domain.Request{
			{ID: "r1", Name: "Get Users", Method: "GET", URL: "http://api/users"},
		}, nil)
	mock.EXPECT().
		ListRequests(gomock.Any(), "col-2").
		Return([]*domain.Request{
			{ID: "r2", Name: "Create Invoice", Method: "POST", URL: "http://api/invoices"},
		}, nil)

	s := search.New(mock)

	result, err := s.SearchAll(context.Background(), "invoice", []string{"col-1", "col-2"})
	require.NoError(t, err)
	require.Len(t, result.Hits, 1)
	assert.Equal(t, "r2", result.Hits[0].Request.ID)
}

func TestSearcher_SearchAll_ContextCanceled_DoesNotError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mock := mocks.NewMockRequestReader(ctrl)
	mock.EXPECT().
		ListRequests(gomock.Any(), "col-1").
		Return([]*domain.Request{
			{ID: "r1", Name: "Create Invoice", Method: "POST", URL: "http://api/invoices"},
		}, nil)
	mock.EXPECT().
		ListRequests(gomock.Any(), "col-2").
		DoAndReturn(func(ctx context.Context, _ string) ([]*domain.Request, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})

	s := search.New(mock)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	result, err := s.SearchAll(ctx, "invoice", []string{"col-1", "col-2"})
	require.NoError(t, err)
	require.Len(t, result.Hits, 1)
	assert.Equal(t, "r1", result.Hits[0].Request.ID)
}

func TestSearcher_DurationTracked(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "Test", Method: "GET", URL: "http://api/test"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "test")
	require.NoError(t, err)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestSearcher_EqualScoreTiebreaker(t *testing.T) {
	ctrl := gomock.NewController(t)
	// Both requests contain "test" in name — will score identically on substring match.
	reqs := []*domain.Request{
		{ID: "zzz", Name: "test-b", Method: "GET", URL: "http://api/b"},
		{ID: "aaa", Name: "test-a", Method: "GET", URL: "http://api/a"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "test")
	require.NoError(t, err)
	require.Len(t, result.Hits, 2)
	// When scores are equal, hits ordered by ID ASC.
	assert.Equal(t, "aaa", result.Hits[0].Request.ID)
	assert.Equal(t, "zzz", result.Hits[1].Request.ID)
}

func TestSearcher_FuzzyInitialsMatchName(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "List Payments", Method: "GET", URL: "http://api/payments"},
		{ID: "r2", Name: "Create Payment", Method: "POST", URL: "http://api/payments"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "lp")
	require.NoError(t, err)
	require.NotEmpty(t, result.Hits)
	assert.Equal(t, "r1", result.Hits[0].Request.ID)
}

func TestSearcher_FuzzyInitialsMatchName_Unicode(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "Crème Brûlée", Method: "GET", URL: "http://api/desserts"},
		{ID: "r2", Name: "Create Billing", Method: "POST", URL: "http://api/billing"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "cb")
	require.NoError(t, err)
	require.NotEmpty(t, result.Hits)
	assert.Equal(t, "r1", result.Hits[0].Request.ID)
}

func TestSearcher_FuzzySubsequence_UnicodeQuery(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "päyments", Method: "GET", URL: "http://api/payments"},
		{ID: "r2", Name: "orders", Method: "GET", URL: "http://api/orders"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "päy")
	require.NoError(t, err)
	require.NotEmpty(t, result.Hits)
	assert.Equal(t, "r1", result.Hits[0].Request.ID)
}

func TestSearcher_MultiTokenPrefixNarrowsResults(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := []*domain.Request{
		{ID: "r1", Name: "Archive Legacy Payment", Method: "GET", URL: "http://api/archive"},
		{ID: "r2", Name: "List Payments", Method: "GET", URL: "http://api/payments"},
	}
	mock := stubRequests(ctrl, reqs)
	s := search.New(mock)

	result, err := s.Search(context.Background(), testCollectionID, "list pay")
	require.NoError(t, err)
	require.Len(t, result.Hits, 1)
	assert.Equal(t, "r2", result.Hits[0].Request.ID)
}

func TestSearcher_1000Requests_Under50ms(t *testing.T) {
	ctrl := gomock.NewController(t)
	reqs := make([]*domain.Request, 1000)
	for i := range reqs {
		reqs[i] = &domain.Request{
			ID:     fmt.Sprintf("r%04d", i),
			Name:   fmt.Sprintf("Request %d", i),
			Method: "GET",
			URL:    fmt.Sprintf("http://api/v1/resource/%d", i),
		}
	}
	mock := mocks.NewMockRequestReader(ctrl)
	mock.EXPECT().
		ListRequests(gomock.Any(), testCollectionID).
		Return(reqs, nil).
		AnyTimes()

	s := search.New(mock)

	start := time.Now()
	result, err := s.Search(context.Background(), testCollectionID, "request")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.NotEmpty(t, result.Hits)
	assert.Less(
		t,
		elapsed,
		50*time.Millisecond,
		"search over 1000 requests must complete in <50ms, took %v",
		elapsed,
	)
}

func BenchmarkSearcher_1000(b *testing.B) {
	ctrl := gomock.NewController(b)
	reqs := make([]*domain.Request, 1000)
	for i := range reqs {
		reqs[i] = &domain.Request{
			ID:     fmt.Sprintf("r%04d", i),
			Name:   fmt.Sprintf("Request %d", i),
			Method: "GET",
			URL:    fmt.Sprintf("http://api/v1/resource/%d", i),
		}
	}
	mock := mocks.NewMockRequestReader(ctrl)
	mock.EXPECT().
		ListRequests(gomock.Any(), testCollectionID).
		Return(reqs, nil).
		AnyTimes()

	s := search.New(mock)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Search(ctx, testCollectionID, "test")
	}
}
