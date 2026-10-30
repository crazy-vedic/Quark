package curl_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
)

func TestExportRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request *domain.Request
		want    string
		wantErr string
	}{
		{
			name:    "nil request",
			wantErr: "request is required",
		},
		{
			name:    "blank URL",
			request: &domain.Request{URL: " \t"},
			wantErr: "request URL is required",
		},
		{
			name:    "defaults method and ignores blank optional fields",
			request: &domain.Request{URL: "https://example.test/path", Method: "  ", Headers: "  ", AuthType: "  "},
			want:    "curl -X 'GET' 'https://example.test/path'",
		},
		{
			name: "trims method, sorts headers, and safely quotes values and body",
			request: &domain.Request{
				Method:  " POST ",
				URL:     "https://example.test/items",
				Headers: `{"Z-Trace":"trace'unsafe","Content-Type":"application/json"}`,
				Body:    `{"name":"O'Reilly"}`,
			},
			want: "curl -X 'POST' -H 'Content-Type: application/json' " +
				"-H 'Z-Trace: trace'\\''unsafe' --data-raw '{\"name\":\"O'\\''Reilly\"}' " +
				"'https://example.test/items'",
		},
		{
			name:    "malformed headers",
			request: &domain.Request{URL: "https://example.test", Headers: "{"},
			wantErr: "parse request headers:",
		},
		{
			name: "bearer authorization",
			request: &domain.Request{
				URL: "https://example.test", AuthType: " BEARER ",
				AuthConfig: `{"token":"secret value"}`,
			},
			want: "curl -X 'GET' -H 'Authorization: Bearer secret value' 'https://example.test'",
		},
		{
			name: "basic authorization",
			request: &domain.Request{
				URL: "https://example.test", AuthType: domain.AuthTypeBasic,
				AuthConfig: `{"username":"api","password":"p'ass"}`,
			},
			want: "curl -X 'GET' -u 'api:p'\\''ass' 'https://example.test'",
		},
		{
			name: "API key defaults to header",
			request: &domain.Request{
				URL: "https://example.test", AuthType: domain.AuthTypeAPIKey,
				AuthConfig: `{"name":"X-Key","value":"key value"}`,
			},
			want: "curl -X 'GET' -H 'X-Key: key value' 'https://example.test'",
		},
		{
			name: "API key query preserves existing values",
			request: &domain.Request{
				URL:        "https://example.test/items?keep=one&tag=two&tag=three",
				AuthType:   domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"api_key","value":"a b&c"}`,
			},
			want: "curl -X 'GET' 'https://example.test/items?api_key=a+b%26c&keep=one&tag=two&tag=three'",
		},
		{
			name: "API key query replaces existing key",
			request: &domain.Request{
				URL:        "https://example.test/items?api_key=old&keep=yes",
				AuthType:   domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"api_key","value":"new"}`,
			},
			want: "curl -X 'GET' 'https://example.test/items?api_key=new&keep=yes'",
		},
		{
			name:    "invalid auth config",
			request: &domain.Request{URL: "https://example.test", AuthType: domain.AuthTypeBearer, AuthConfig: "{"},
			wantErr: "parse request authorization:",
		},
		{
			name: "invalid URL for query API key",
			request: &domain.Request{
				URL: "https://example.test/%zz", AuthType: domain.AuthTypeAPIKey,
				AuthConfig: `{"in":"query","name":"key","value":"value"}`,
			},
			wantErr: "parse request URL:",
		},
		{
			name:    "unsupported authorization type",
			request: &domain.Request{URL: "https://example.test", AuthType: "digest", AuthConfig: `{}`},
			wantErr: `unsupported authorization type "digest"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := curl.ExportRequest(tc.request)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
