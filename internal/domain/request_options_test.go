package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(v bool) *bool { return &v }
func intPtr(v int) *int    { return &v }

func TestRequestOptions_ZeroValueEncodesAsEmptyObject(t *testing.T) {
	encoded, err := RequestOptions{}.JSON()
	require.NoError(t, err)
	assert.Equal(t, "{}", encoded)
	assert.True(t, RequestOptions{}.IsZero())

	parsed, err := ParseRequestOptions(encoded)
	require.NoError(t, err)
	assert.True(t, parsed.IsZero())

	parsed, err = ParseRequestOptions("   ")
	require.NoError(t, err)
	assert.True(t, parsed.IsZero())
}

func TestRequestOptions_RoundTripsAllFields(t *testing.T) {
	in := RequestOptions{
		TimeoutMS:       2500,
		FollowRedirects: boolPtr(false),
		MaxRedirects:    intPtr(0),
		Proxy:           "socks5h://proxy.internal:1080",
		SkipTLSVerify:   true,
	}
	encoded, err := in.JSON()
	require.NoError(t, err)

	out, err := ParseRequestOptions(encoded)
	require.NoError(t, err)
	assert.Equal(t, 2500*time.Millisecond, out.Timeout())
	require.NotNil(t, out.FollowRedirects)
	assert.False(t, *out.FollowRedirects)
	require.NotNil(t, out.MaxRedirects, "an explicit zero max is kept, not treated as unset")
	assert.Equal(t, 0, *out.MaxRedirects)
	assert.Equal(t, in.Proxy, out.Proxy)
	assert.True(t, out.SkipTLSVerify)
}

func TestParseRequestOptions_RejectsUnknownFieldsAndBadValues(t *testing.T) {
	cases := map[string]string{
		"unknown field":      `{"no_verify_hostname": true}`,
		"not an object":      `[1,2]`,
		"negative timeout":   `{"timeout_ms": -1}`,
		"negative redirects": `{"max_redirects": -2}`,
		"bad proxy scheme":   `{"proxy": "ftp://files.example"}`,
		"proxy without host": `{"proxy": "http://"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseRequestOptions(raw)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidRequestOptions))
		})
	}
}

func TestNormalizeProxyURL(t *testing.T) {
	got, err := NormalizeProxyURL("proxy.internal:8080")
	require.NoError(t, err)
	assert.Equal(t, "http://proxy.internal:8080", got, "a bare host:port gets the http scheme")

	got, err = NormalizeProxyURL("  https://p.example:443  ")
	require.NoError(t, err)
	assert.Equal(t, "https://p.example:443", got)

	got, err = NormalizeProxyURL("")
	require.NoError(t, err)
	assert.Empty(t, got, "blank means no per-request proxy")

	_, err = NormalizeProxyURL("http://[::1")
	assert.True(t, errors.Is(err, ErrInvalidRequestOptions))
}

func TestRequestOptions_ValidateAcceptsDefaults(t *testing.T) {
	assert.NoError(t, RequestOptions{}.Validate())
	assert.NoError(t, RequestOptions{TimeoutMS: 1, MaxRedirects: intPtr(3)}.Validate())
}

func TestRequestOptions_SummaryHidesProxyPassword(t *testing.T) {
	const proxy = "http://corp-user:hunter2@proxy.internal:3128" //nolint:gosec // fake password in a fixture
	opts := RequestOptions{Proxy: proxy}

	summary := opts.Summary()
	assert.Contains(t, summary, "proxy.internal:3128")
	assert.NotContains(t, summary, "hunter2", "a password must not reach the screen")
	assert.Equal(t, "http://corp-user:xxxxx@proxy.internal:3128", RedactProxyURL(opts.Proxy))
	assert.Equal(t, "http://corp-user:hunter2@proxy.internal:3128", opts.Proxy,
		"redaction is for display only; the stored value is unchanged")
}

func TestRequestOptions_SummaryListsOnlySetOptions(t *testing.T) {
	assert.Empty(t, RequestOptions{}.Summary())

	follow := false
	got := RequestOptions{
		TimeoutMS:       2500,
		FollowRedirects: &follow,
		MaxRedirects:    intPtr(2),
		Proxy:           "socks5://127.0.0.1:1080",
		SkipTLSVerify:   true,
	}.Summary()
	assert.Equal(t,
		"timeout 2.5s, no redirects, max 2 redirects, "+
			"proxy socks5://127.0.0.1:1080, TLS verification off",
		got)
}
