package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestEnvironment_IsGlobal(t *testing.T) {
	tests := []struct {
		name         string
		collectionID string
		want         bool
	}{
		{"global", "", true},
		{"collection", "col-123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &domain.Environment{CollectionID: tt.collectionID}
			assert.Equal(t, tt.want, e.IsGlobal())
		})
	}
}

func TestEnvironment_Vars(t *testing.T) {
	tests := []struct {
		name string
		data string
		want map[string]string
	}{
		{"empty", "", nil},
		{"empty object", "{}", nil},
		{
			"valid",
			`{"url": "http://localhost/", "key": "val"}`,
			map[string]string{"url": "http://localhost/", "key": "val"},
		},
		{"invalid json", "not-json", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &domain.Environment{Data: tt.data}
			assert.Equal(t, tt.want, e.Vars())
		})
	}
}

func TestEnvironment_SetVars(t *testing.T) {
	e := &domain.Environment{}

	e.SetVars(map[string]string{"url": "http://localhost/"})
	assert.Equal(t, `{"url":"http://localhost/"}`, e.Data)

	e.SetVars(nil)
	assert.Equal(t, "{}", e.Data)

	e.SetVars(map[string]string{})
	assert.Equal(t, "{}", e.Data)
}

func TestEnvironment_VarsRoundTrip(t *testing.T) {
	original := map[string]string{
		"url":       "http://localhost/",
		"api_key":   "secret123",
		"empty":     "",
		"withSpace": "hello world",
	}
	e := &domain.Environment{}
	e.SetVars(original)
	got := e.Vars()
	assert.Equal(t, original, got)
}
