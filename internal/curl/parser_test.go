package curl

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendQueryPreservesFragment(t *testing.T) {
	got := appendQuery("https://example.com/path#fragment", "a=1")
	if want := "https://example.com/path?a=1#fragment"; got != want {
		t.Fatalf("appendQuery() = %q, want %q", got, want)
	}
}

// Flags that only affect curl's own output or connection handling are common
// in copied snippets. They carry nothing to import, so they must be skipped
// with a warning instead of failing the whole import.
func TestParse_IgnoresHarmlessCurlFlags(t *testing.T) {
	t.Parallel()

	commands := []string{
		`curl -i https://example.test/a`,
		`curl --include https://example.test/a`,
		`curl -N https://example.test/a`,
		`curl --no-buffer https://example.test/a`,
		`curl --http1.1 https://example.test/a`,
		`curl --http2 https://example.test/a`,
		`curl -g https://example.test/a`,
		`curl --globoff https://example.test/a`,
		`curl -# https://example.test/a`,
		`curl --progress-bar https://example.test/a`,
		`curl --no-progress-meter https://example.test/a`,
		`curl -4 https://example.test/a`,
		`curl -6 https://example.test/a`,
		`curl --tlsv1.2 https://example.test/a`,
		`curl --path-as-is https://example.test/a`,
		`curl -w '%{http_code}' https://example.test/a`,
		`curl --write-out '%{http_code}' https://example.test/a`,
		`curl -D headers.txt https://example.test/a`,
		`curl --dump-header headers.txt https://example.test/a`,
		`curl --max-redirs 5 https://example.test/a`,
		`curl -sSi https://example.test/a`,
	}
	for _, command := range commands {
		result, err := NewImporter().Parse(strings.NewReader(command))
		require.NoError(t, err, command)
		require.Equal(t, "https://example.test/a", result.URL, command)
		require.Equal(t, "GET", result.Method, command)
	}
}

func TestParse_MapsRequestOptionFlags(t *testing.T) {
	result, err := NewImporter().Parse(strings.NewReader(
		`curl -k -L --max-redirs 3 -x proxy.internal:3128 -m 2.5 https://api.example.com`,
	))
	require.NoError(t, err)

	require.True(t, result.Options.SkipTLSVerify)
	require.NotNil(t, result.Options.FollowRedirects)
	assert.True(t, *result.Options.FollowRedirects)
	require.NotNil(t, result.Options.MaxRedirects)
	assert.Equal(t, 3, *result.Options.MaxRedirects)
	assert.Equal(t, "http://proxy.internal:3128", result.Options.Proxy,
		"a bare host:port gets the http scheme, as curl treats it")
	assert.Equal(t, 2500, result.Options.TimeoutMS, "-m takes decimal seconds")
	assert.Equal(t, Review, result.Security, "-k changes TLS checks, so it needs review")
	assert.Empty(t, result.Warnings)
}

func TestParse_LongRequestOptionFlagsAndSocksProxy(t *testing.T) {
	result, err := NewImporter().Parse(strings.NewReader(
		`curl --insecure --location --max-time 30 --proxy socks5h://127.0.0.1:1080 https://api.example.com`,
	))
	require.NoError(t, err)

	assert.True(t, result.Options.SkipTLSVerify)
	require.NotNil(t, result.Options.FollowRedirects)
	assert.True(t, *result.Options.FollowRedirects)
	assert.Equal(t, 30000, result.Options.TimeoutMS)
	assert.Equal(t, "socks5h://127.0.0.1:1080", result.Options.Proxy)
}

func TestParse_NoRequestOptionFlagsKeepDefaults(t *testing.T) {
	result, err := NewImporter().Parse(strings.NewReader(`curl https://api.example.com`))
	require.NoError(t, err)
	assert.True(t, result.Options.IsZero())
}

func TestParse_ZeroMaxTimeAndUnlimitedRedirectsKeepDefaults(t *testing.T) {
	result, err := NewImporter().Parse(strings.NewReader(
		`curl -m 0 --max-redirs -1 https://api.example.com`,
	))
	require.NoError(t, err)
	assert.Zero(t, result.Options.TimeoutMS, "curl's zero means no limit: the default")
	assert.Nil(t, result.Options.MaxRedirects)
	assert.Equal(t, []string{
		"unlimited redirects (--max-redirs -1) is not imported; Quark's configured redirect limit applies",
	}, result.Warnings)
}

func TestParse_RejectsRequestOptionValuesItCannotApply(t *testing.T) {
	cases := map[string]string{
		"unsupported proxy scheme": `curl -x socks4://127.0.0.1:1080 https://api.example.com`,
		"non-numeric max-time":     `curl -m soon https://api.example.com`,
		"negative max-time":        `curl --max-time -2 https://api.example.com`,
		"non-numeric max-redirs":   `curl --max-redirs many https://api.example.com`,
	}
	for name, command := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewImporter().Parse(strings.NewReader(command))
			require.Error(t, err, "a flag that cannot be applied must not be dropped silently")
		})
	}
}
