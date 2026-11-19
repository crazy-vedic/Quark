//go:build e2e

package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// runQuarkOnTerminal runs the binary with a pseudo-terminal as stdout, using
// util-linux script(1), so the terminal-only output path runs.
func runQuarkOnTerminal(t *testing.T, home string, args ...string) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("needs util-linux script(1) to allocate a pseudo-terminal")
	}
	scriptPath, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script(1) not available")
	}
	command := binaryPath
	for _, arg := range args {
		command += " '" + arg + "'"
	}
	cmd := exec.Command(scriptPath, "-qec", command, "/dev/null")
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "NO_COLOR=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Run(), "quark failed on a terminal: %s", out.String())
	return out.String()
}

// A server controls the response body, so on a terminal the CLI must not pass
// control characters through: they can ring the bell, move the cursor or
// rewrite earlier output. Printable text, tabs and newlines are kept, including
// characters whose UTF-8 bytes fall in the 0x80-0x9f range (such as U+2192).
func TestCLI_RunOnTerminalStripsControlCharactersFromBody(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(
			"start\a\b\x7f|\x1b[2J|\x1b]0;title\a|\xc2\x85\xc2\x9b|tab\tkept|arrow → kept|end",
		))
	}))
	t.Cleanup(srv.Close)
	seedRequest(t, home, colID, &domain.Request{
		Name: "Hostile", Method: http.MethodGet, URL: srv.URL + "/body",
	})

	out := runQuarkOnTerminal(t, home, "run", "API/Hostile")

	for _, bad := range []string{"\a", "\b", "\x7f", "\x1b[2J", "\x1b]0;", "\xc2\x85", "\xc2\x9b"} {
		assert.NotContains(t, out, bad, "control sequence %q must not reach the terminal", bad)
	}
	assert.Contains(t, out, "start")
	assert.Contains(t, out, "tab\tkept")
	assert.Contains(t, out, "arrow → kept")
	assert.Contains(t, out, "end")
}
