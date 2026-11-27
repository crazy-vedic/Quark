package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestGraphQLEditorText_RoundTrip(t *testing.T) {
	body := domain.GraphQLBody{
		Query:     "query Q($id: ID!) {\n  user(id: $id) { name }\n}",
		Variables: `{"id": "1"}`,
	}
	text := domain.GraphQLEditorText(body)
	assert.Equal(
		t,
		"query Q($id: ID!) {\n  user(id: $id) { name }\n}\n# --- variables ---\n{\"id\": \"1\"}",
		text,
	)
	assert.Equal(t, body, domain.GraphQLFromEditorText(text))

	noVars := domain.GraphQLBody{Query: "{ me { id } }"}
	assert.Equal(t, "{ me { id } }", domain.GraphQLEditorText(noVars))
	assert.Equal(t, noVars, domain.GraphQLFromEditorText("{ me { id } }"))
}

func TestGraphQLFromEditorText_SeparatorOnlyWhenWholeLine(t *testing.T) {
	got := domain.GraphQLFromEditorText("# not --- variables ---\n{ a }")
	assert.Equal(t, domain.GraphQLBody{Query: "# not --- variables ---\n{ a }"}, got)
}

func TestMarshalGraphQLBody(t *testing.T) {
	stored, err := domain.MarshalGraphQLBody(
		domain.GraphQLBody{Query: "{ a }", Variables: `{"x":1}`},
	)
	require.NoError(t, err)
	parsed, err := domain.ParseGraphQLBody(stored)
	require.NoError(t, err)
	assert.Equal(t, domain.GraphQLBody{Query: "{ a }", Variables: `{"x":1}`}, parsed)

	empty, err := domain.MarshalGraphQLBody(domain.GraphQLBody{})
	require.NoError(t, err)
	assert.Empty(t, empty)

	_, err = domain.ParseGraphQLBody("{bad")
	assert.ErrorContains(t, err, "graphql body is not valid")
}

func TestGraphQLRequestPayload(t *testing.T) {
	stored, err := domain.MarshalGraphQLBody(domain.GraphQLBody{
		Query:     `query Q { a(x: "<b>&") }`,
		Variables: "{\n  \"id\": 7\n}",
	})
	require.NoError(t, err)
	payload, err := domain.GraphQLRequestPayload(stored)
	require.NoError(t, err)
	assert.Equal(t, `{"query":"query Q { a(x: \"<b>&\") }","variables":{"id":7}}`, payload)

	noVars, err := domain.MarshalGraphQLBody(domain.GraphQLBody{Query: "{ me { id } }"})
	require.NoError(t, err)
	payload, err = domain.GraphQLRequestPayload(noVars)
	require.NoError(t, err)
	assert.Equal(t, `{"query":"{ me { id } }"}`, payload)

	payload, err = domain.GraphQLRequestPayload("")
	require.NoError(t, err)
	assert.Empty(t, payload, "a blank body sends nothing")
}

func TestGraphQLRequestPayload_RefusesVariablesThatAreNotAnObject(t *testing.T) {
	for _, vars := range []string{`[1,2]`, `"text"`, `{"id":`} {
		stored, err := domain.MarshalGraphQLBody(
			domain.GraphQLBody{Query: "{ a }", Variables: vars},
		)
		require.NoError(t, err)
		_, err = domain.GraphQLRequestPayload(stored)
		assert.ErrorContains(t, err, "variables must be a JSON object", vars)
	}
}
