package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyName(t *testing.T) {
	none := func(string) bool { return false }
	assert.Equal(t, "List copy", CopyName("List", none))

	taken := map[string]bool{"List copy": true}
	assert.Equal(t, "List copy 2", CopyName("List", func(n string) bool { return taken[n] }))

	taken["List copy 2"] = true
	assert.Equal(t, "List copy 3", CopyName("List", func(n string) bool { return taken[n] }))

	// A gap is reused: only the names that are taken block a candidate.
	taken = map[string]bool{"List copy 2": true}
	assert.Equal(t, "List copy", CopyName("List", func(n string) bool { return taken[n] }))
}
