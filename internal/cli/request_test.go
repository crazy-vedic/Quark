package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

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
