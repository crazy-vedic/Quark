package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestValidMethod(t *testing.T) {
	valid := []string{
		"GET",
		"POST",
		"TRACE",
		"PROPFIND",
		"QUERY",
		"MKCOL",
		"X-CUSTOM",
		"M_1",
		"patch",
	}
	for _, m := range valid {
		assert.NoError(t, domain.ValidMethod(m), m)
	}
	invalid := []string{"", " ", "GET ", "GE T", "GET\n", "GET/1", "(GET)", "GÉT", "GET:"}
	for _, m := range invalid {
		assert.ErrorIs(t, domain.ValidMethod(m), domain.ErrInvalidMethod, "%q", m)
	}
}
