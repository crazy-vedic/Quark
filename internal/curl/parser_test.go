package curl

import (
	"strings"
	"testing"

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
