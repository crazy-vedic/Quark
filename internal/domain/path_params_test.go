package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestPathPlaceholders(t *testing.T) {
	assert.Equal(t, []string{"id", "sub"},
		domain.PathPlaceholders("https://api.test/users/:id/items/:sub?x=:nope#:frag"))
	assert.Equal(t, []string{"id"},
		domain.PathPlaceholders("https://api.test/users/:id/:id"))
	assert.Equal(t, []string{"base_id"},
		domain.PathPlaceholders("{{base}}/:base_id"))
	assert.Equal(t, []string{"port"},
		domain.PathPlaceholders("http://localhost:8080/:port"))
}

func TestPathPlaceholders_IgnoresNonPlaceholders(t *testing.T) {
	for _, raw := range []string{
		"https://api.test/",
		"https://api.test",
		"https://api.test/a:b/c",
		"https://api.test/:",
		"https://api.test/:1x",
		"https://api.test/:a.json",
		"https://api.test/:{{id}}",
		"https://api.test/?q=:id",
	} {
		assert.Empty(t, domain.PathPlaceholders(raw), raw)
	}
}

func TestApplyPathParams(t *testing.T) {
	values := map[string]string{"id": "42", "sub": "a b/c"}
	assert.Equal(t, "https://api.test/users/42/items/a%20b%2Fc?x=:id#:id",
		domain.ApplyPathParams("https://api.test/users/:id/items/:sub?x=:id#:id", values))
	assert.Equal(t, "https://api.test/users/:id",
		domain.ApplyPathParams("https://api.test/users/:id", map[string]string{"id": ""}))
	assert.Equal(t, "https://api.test/users/:id",
		domain.ApplyPathParams("https://api.test/users/:id", nil))
	assert.Equal(t, "https://api.test",
		domain.ApplyPathParams("https://api.test", values))
}

func TestApplyPathParams_KeepsVariablesAndEscapesLeadingColon(t *testing.T) {
	got := domain.ApplyPathParams("{{base}}/users/:id", map[string]string{"id": "{{user_id}}"})
	assert.Equal(t, "{{base}}/users/{{user_id}}", got)

	got = domain.ApplyPathParams("https://api.test/:id", map[string]string{"id": ":other"})
	assert.Equal(t, "https://api.test/%3Aother", got)
	// Applying again is a no-op, so a value never gets substituted twice.
	again := domain.ApplyPathParams(got, map[string]string{"other": "x", "id": ":other"})
	assert.Equal(t, got, again)
}
