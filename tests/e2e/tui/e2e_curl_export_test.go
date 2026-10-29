//go:build e2e

package tui_test

import (
	"context"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestE2E_CurlExportFromPersistedRequest(t *testing.T) {
	collection := &domain.Collection{ID: "curl-export", Name: "Curl export"}
	st := setupStore(t, collection)
	request := &domain.Request{
		ID:         "complex-request",
		Name:       "Create item",
		Method:     "POST",
		URL:        "https://api.example.test/items?existing=one",
		Headers:    `{"Z-Trace":"trace'$(echo unsafe)","Content-Type":"application/json"}`,
		AuthType:   domain.AuthTypeBearer,
		AuthConfig: `{"token":"s e'cret$"}`,
		Body:       "{\"name\":\"O'Reilly\",\"literal\":\"$HOME `id`\"}\nsecond line",
	}
	seedRequests(t, st, collection.ID, request)

	persisted, err := st.ListRequests(context.Background(), collection.ID)
	require.NoError(t, err)
	require.Len(t, persisted, 1)
	command, err := curl.ExportRequest(persisted[0])
	require.NoError(t, err)
	require.Equal(t,
		"curl -X 'POST' -H 'Content-Type: application/json' "+
			"-H 'Z-Trace: trace'\\''$(echo unsafe)' "+
			"-H 'Authorization: Bearer s e'\\''cret$' "+
			"--data-raw '{\"name\":\"O'\\''Reilly\",\"literal\":\"$HOME `id`\"}\nsecond line' "+
			"'https://api.example.test/items?existing=one'",
		command,
	)

	model := newE2EModel(t, st, &mockExecutor{}).
		WithActiveRequest(persisted[0]).
		WithFocus(tui.RequestPane)
	_, shortcutCmd := callUpdateWithCmd(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.NotNil(t, shortcutCmd, "x should dispatch the curl export clipboard action")

	_, paletteCmd := choosePaletteCommand(t, model, "copy request as curl")
	require.NotNil(t, paletteCmd, "the command palette should dispatch the curl export clipboard action")
}

func TestE2E_CurlExportAuthAndURLVariants(t *testing.T) {
	collection := &domain.Collection{ID: "curl-export-variants", Name: "Curl export variants"}
	st := setupStore(t, collection)
	cases := []struct {
		name      string
		method    string
		url       string
		authType  string
		authJSON  string
		wantParts []string
	}{
		{
			name:     "basic auth with punctuation",
			method:   "GET",
			url:      "https://api.example.test/private",
			authType: domain.AuthTypeBasic,
			authJSON: `{"username":"api:user","password":"p a'ss"}`,
			wantParts: []string{
				"-u 'api:user:p a'\\''ss'",
				"'https://api.example.test/private'",
			},
		},
		{
			name:     "API key header",
			method:   "DELETE",
			url:      "https://api.example.test/items/3",
			authType: domain.AuthTypeAPIKey,
			authJSON: `{"in":"header","name":"X-API-Key","value":"key with spaces"}`,
			wantParts: []string{
				"curl -X 'DELETE'",
				"-H 'X-API-Key: key with spaces'",
				"'https://api.example.test/items/3'",
			},
		},
		{
			name:     "API key query preserves existing values",
			method:   "GET",
			url:      "https://api.example.test/items?keep=two+words&tag=one&tag=two",
			authType: domain.AuthTypeAPIKey,
			authJSON: `{"in":"query","name":"api_key","value":"a b&c"}`,
			wantParts: []string{
				"'https://api.example.test/items?api_key=a+b%26c&keep=two+words&tag=one&tag=two'",
			},
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := &domain.Request{
				ID:         fmt.Sprintf("variant-%d", i),
				Name:       tc.name,
				Method:     tc.method,
				URL:        tc.url,
				AuthType:   tc.authType,
				AuthConfig: tc.authJSON,
			}
			seedRequests(t, st, collection.ID, request)
			persisted, err := st.ListRequests(context.Background(), collection.ID)
			require.NoError(t, err)
			var saved *domain.Request
			for _, candidate := range persisted {
				if candidate.ID == request.ID {
					saved = candidate
					break
				}
			}
			require.NotNil(t, saved)

			command, err := curl.ExportRequest(saved)
			require.NoError(t, err)
			for _, part := range tc.wantParts {
				require.Contains(t, command, part)
			}
		})
	}
}

func TestE2E_CurlExportDefaultsMissingMethod(t *testing.T) {
	command, err := curl.ExportRequest(&domain.Request{URL: "https://api.example.test/health"})
	require.NoError(t, err)
	require.Equal(t, "curl -X 'GET' 'https://api.example.test/health'", command)
}

func TestE2E_CurlExportRejectsInvalidPersistedRequests(t *testing.T) {
	collection := &domain.Collection{ID: "curl-export-invalid", Name: "Invalid curl exports"}
	st := setupStore(t, collection)
	cases := []struct {
		name    string
		url     string
		headers string
		auth    string
		config  string
		wantErr string
	}{
		{name: "empty URL", wantErr: "request URL is required"},
		{name: "malformed headers", url: "https://example.test", headers: "{", wantErr: "parse request headers"},
		{name: "malformed auth config", url: "https://example.test", auth: domain.AuthTypeBearer, config: "{", wantErr: "parse request authorization"},
		{name: "unsupported auth type", url: "https://example.test", auth: "digest", config: `{}`, wantErr: `unsupported authorization type "digest"`},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := &domain.Request{
				ID:         fmt.Sprintf("invalid-%d", i),
				Name:       tc.name,
				Method:     "GET",
				URL:        tc.url,
				Headers:    tc.headers,
				AuthType:   tc.auth,
				AuthConfig: tc.config,
			}
			seedRequests(t, st, collection.ID, request)
			persisted, err := st.ListRequests(context.Background(), collection.ID)
			require.NoError(t, err)
			var saved *domain.Request
			for _, candidate := range persisted {
				if candidate.ID == request.ID {
					saved = candidate
					break
				}
			}
			require.NotNil(t, saved)

			command, err := curl.ExportRequest(saved)
			require.ErrorContains(t, err, tc.wantErr)
			require.Empty(t, command)
		})
	}

	_, err := curl.ExportRequest(nil)
	require.EqualError(t, err, "request is required")
}
