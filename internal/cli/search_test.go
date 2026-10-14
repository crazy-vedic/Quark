package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestNewSearchCmd_PrintsFuzzyMatchesAcrossCollections(t *testing.T) {
	st := &fakeRunStore{
		collections: []*domain.Collection{{ID: "col-1", Name: "Payments"}},
		requests: map[string][]*domain.Request{
			"col-1": {
				{
					ID:           "req-1",
					CollectionID: "col-1",
					Name:         "List Payments",
					Method:       "GET",
					URL:          "https://api.test/payments",
				},
				{
					ID:           "req-2",
					CollectionID: "col-1",
					Name:         "Create Order",
					Method:       "POST",
					URL:          "https://api.test/orders",
				},
			},
		},
	}

	cmd := NewSearchCmd(st)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"lp"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Payments/List Payments")
	assert.NotContains(t, out.String(), "Create Order")
}

type searchErrorStore struct {
	*fakeRunStore
	collectionErr error
	requestErr    error
}

func (s searchErrorStore) ListCollections(ctx context.Context) ([]*domain.Collection, error) {
	if s.collectionErr != nil {
		return nil, s.collectionErr
	}
	return s.fakeRunStore.ListCollections(ctx)
}

func (s searchErrorStore) ListRequests(ctx context.Context, id string) ([]*domain.Request, error) {
	if s.requestErr != nil {
		return nil, s.requestErr
	}
	return s.fakeRunStore.ListRequests(ctx, id)
}

func TestNewSearchCmd_ErrorAndEmptyResults(t *testing.T) {
	for _, tt := range []struct {
		name string
		st   *searchErrorStore
		want string
	}{
		{"list failure", &searchErrorStore{fakeRunStore: &fakeRunStore{}, collectionErr: errors.New("list failed")}, "search: list collections"},
		{
			"request read failure is skipped",
			&searchErrorStore{
				fakeRunStore: &fakeRunStore{collections: []*domain.Collection{{ID: "c", Name: "C"}}},
				requestErr:   errors.New("read failed"),
			},
			"No requests found.",
		},
		{"no matches", &searchErrorStore{fakeRunStore: &fakeRunStore{}}, "No requests found."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewSearchCmd(tt.st)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"absent"})
			cmd.SetContext(context.Background())
			err := cmd.Execute()
			if tt.name == "no matches" || tt.name == "request read failure is skipped" {
				require.NoError(t, err)
				require.Contains(t, out.String(), tt.want)
			} else {
				require.ErrorContains(t, err, tt.want)
			}
		})
	}
}
