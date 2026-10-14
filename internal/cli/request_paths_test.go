package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestRequestListBranches(t *testing.T) {
	tests := []struct {
		name    string
		store   *fakeRunStore
		args    []string
		wantErr string
		wantOut string
	}{
		{
			name: "missing collection", store: &fakeRunStore{},
			args: []string{"list"}, wantErr: "--collection is required",
		},
		{
			name: "unknown collection", store: &fakeRunStore{},
			args:    []string{"list", "--collection", "missing"},
			wantErr: "request list: resolve collection",
		},
		{
			name: "empty list",
			store: &fakeRunStore{
				collections: []*domain.Collection{{ID: "c1", Name: "Payments"}},
			},
			args:    []string{"list", "--collection", "Payments"},
			wantOut: "No requests found.",
		},
		{
			name: "prints requests",
			store: &fakeRunStore{
				collections: []*domain.Collection{{ID: "collection-1", Name: "Payments"}},
				requests: map[string][]*domain.Request{
					"collection-1": {{ID: "request-1", Method: "GET", Name: "List payments"}},
				},
			},
			args:    []string{"list", "--collection", "Payments"},
			wantOut: "request-  GET     List payments",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRequestCmd(tt.store)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tt.args)
			cmd.SetContext(context.Background())
			err := cmd.Execute()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Contains(t, out.String(), tt.wantOut)
			}
		})
	}
}
