package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// GraphQLVariablesSeparator is the line that divides a GraphQL body's query
// from its variables in the body editor. Because it is a GraphQL comment, the
// query stays valid GraphQL if it is ever read as the query alone.
const GraphQLVariablesSeparator = "# --- variables ---"

// GraphQLBody is the stored form of a graphql body. Query and Variables may
// use {{variables}}. Variables is JSON text, and blank means none.
type GraphQLBody struct {
	Query     string `json:"query"`
	Variables string `json:"variables,omitempty"`
}

// ParseGraphQLBody decodes Request.Body for the graphql body type. A blank
// body is an empty GraphQLBody.
func ParseGraphQLBody(stored string) (GraphQLBody, error) {
	var body GraphQLBody
	if strings.TrimSpace(stored) == "" {
		return body, nil
	}
	if err := json.Unmarshal([]byte(stored), &body); err != nil {
		return GraphQLBody{}, fmt.Errorf("graphql body is not valid: %w", err)
	}
	return body, nil
}

// MarshalGraphQLBody encodes body for Request.Body. An empty body encodes as "".
func MarshalGraphQLBody(body GraphQLBody) (string, error) {
	if body.Query == "" && body.Variables == "" {
		return "", nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode graphql body: %w", err)
	}
	return string(data), nil
}

// GraphQLEditorText shows body as the body editor's text: the query, then the
// separator and the variables when there are any.
func GraphQLEditorText(body GraphQLBody) string {
	if strings.TrimSpace(body.Variables) == "" {
		return body.Query
	}
	return body.Query + "\n" + GraphQLVariablesSeparator + "\n" + body.Variables
}

// GraphQLFromEditorText reads the body editor's text back. The first line
// that is exactly the separator starts the variables; without one, the whole
// text is the query.
func GraphQLFromEditorText(text string) GraphQLBody {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != GraphQLVariablesSeparator {
			continue
		}
		return GraphQLBody{
			Query:     strings.Join(lines[:i], "\n"),
			Variables: strings.Join(lines[i+1:], "\n"),
		}
	}
	return GraphQLBody{Query: text}
}

// GraphQLRequestPayload returns the application/json body that is sent for a
// stored graphql body: {"query": ..., "variables": {...}}. Variables are
// omitted when blank. Variables that are not a JSON object are an error, so
// nothing is sent with a payload the server would reject. A blank query and
// no variables send no body.
func GraphQLRequestPayload(stored string) (string, error) {
	body, err := ParseGraphQLBody(stored)
	if err != nil {
		return "", err
	}
	if body.Query == "" && strings.TrimSpace(body.Variables) == "" {
		return "", nil
	}
	type payload struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables,omitempty"`
	}
	out := payload{Query: body.Query}
	if vars := strings.TrimSpace(body.Variables); vars != "" {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(
			[]byte(vars),
			&object,
		); err != nil ||
			object == nil && vars != "null" {
			return "", fmt.Errorf("graphql variables must be a JSON object")
		}
		out.Variables = json.RawMessage(vars)
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(out); err != nil {
		return "", fmt.Errorf("encode graphql payload: %w", err)
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
