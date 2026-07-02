package cli

import (
	"bytes"
	"context"
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
