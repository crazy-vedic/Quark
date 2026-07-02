package postman

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvironment(t *testing.T) {
	json := `{
		"id": "env-123",
		"name": "Development",
		"values": [
			{"key": "url", "value": "http://localhost/", "enabled": true},
			{"key": "api_key", "value": "secret", "enabled": false},
			{"key": "port", "value": "8080", "enabled": true}
		]
	}`

	env, err := ParseEnvironment(strings.NewReader(json))
	require.NoError(t, err)
	assert.Equal(t, "env-123", env.ID)
	assert.Equal(t, "Development", env.Name)
	require.Len(t, env.Values, 3)
	assert.Equal(t, "url", env.Values[0].Key)
	assert.Equal(t, "http://localhost/", env.Values[0].Value)
	assert.True(t, env.Values[0].Enabled)
	assert.Equal(t, "api_key", env.Values[1].Key)
	assert.False(t, env.Values[1].Enabled)
}

func TestParseEnvironment_MissingName(t *testing.T) {
	json := `{
		"id": "env-123",
		"values": []
	}`

	_, err := ParseEnvironment(strings.NewReader(json))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing name")
}

func TestParseEnvironment_InvalidJSON(t *testing.T) {
	_, err := ParseEnvironment(strings.NewReader("not-json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse environment")
}

func TestEnvironment_ToMap(t *testing.T) {
	env := &Environment{
		Values: []EnvValue{
			{Key: "url", Value: "http://localhost/", Enabled: true},
			{Key: "api_key", Value: "secret", Enabled: false},
			{Key: "port", Value: "8080", Enabled: true},
		},
	}

	m := env.ToMap()
	assert.Equal(t, map[string]string{
		"url":  "http://localhost/",
		"port": "8080",
	}, m)
	assert.NotContains(t, m, "api_key", "disabled values should be skipped")
}

func TestEnvironment_ToMap_Empty(t *testing.T) {
	env := &Environment{Values: []EnvValue{}}
	m := env.ToMap()
	assert.Empty(t, m)
}
