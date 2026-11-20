package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestParseHeaders(t *testing.T) {
	t.Parallel()

	got, err := domain.ParseHeaders(`{"a":"1","B":["2","3"],"c":[]}`)
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"a": {"1"}, "B": {"2", "3"}, "c": {}}, got)

	got, err = domain.ParseHeaders("  ")
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = domain.ParseHeaders(`{"a":1}`)
	require.ErrorContains(t, err, `header "a"`)

	_, err = domain.ParseHeaders(`not json`)
	require.Error(t, err)
}
