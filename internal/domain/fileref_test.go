package domain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestParseFileRef(t *testing.T) {
	cases := []struct {
		value   string
		path    string
		isFile  bool
		literal string
	}{
		{"", "", false, ""},
		{"plain", "", false, "plain"},
		{"@", "", false, "@"},
		{"@payload.json", "payload.json", true, ""},
		{"@/abs/file", "/abs/file", true, ""},
		{"@@", "", false, "@"},
		{"@@alice", "", false, "@alice"},
		{"@@@x", "", false, "@@x"},
		{"a@b", "", false, "a@b"},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			path, isFile, literal := domain.ParseFileRef(tc.value)
			assert.Equal(t, tc.path, path)
			assert.Equal(t, tc.isFile, isFile)
			assert.Equal(t, tc.literal, literal)
		})
	}
}

func TestEscapeFileRef_RoundTripsLiterals(t *testing.T) {
	for _, literal := range []string{"", "x", "@", "@alice", "@@x", "a@b", "@channel deploy done"} {
		stored := domain.EscapeFileRef(literal)
		_, isFile, got := domain.ParseFileRef(stored)
		assert.False(t, isFile, literal)
		assert.Equal(t, literal, got, literal)
	}
	assert.Equal(t, "@@alice", domain.EscapeFileRef("@alice"))
	assert.Equal(t, "@", domain.EscapeFileRef("@"))
}

func TestResolveFileRefPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cfg")
	abs := filepath.Join(t.TempDir(), "abs.json")
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(base, "payload.json"),
		domain.ResolveFileRefPath(base, "payload.json"))
	assert.Equal(t, filepath.Join(base, "dir", "p.json"),
		domain.ResolveFileRefPath(base, "dir/p.json"))
	assert.Equal(t, abs, domain.ResolveFileRefPath(base, abs))
	assert.Equal(t, filepath.Join(home, "p.json"), domain.ResolveFileRefPath(base, "~/p.json"))
	assert.Equal(t, "rel.json", domain.ResolveFileRefPath("", "rel.json"))
}
