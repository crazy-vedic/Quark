//go:build e2e

package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ansiEscape matches the terminal control sequences the TUI writes around its
// text, so assertions can look at the labels the user sees.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// tuiKey is one step of a terminal session: the input sent, then a pause to
// let the TUI draw.
type tuiKey struct {
	input string
	pause time.Duration
}

// runQuarkTUI starts the bare binary, which opens the TUI, on a sized
// pseudo-terminal, sends each step in turn, and returns the terminal output
// with control sequences removed. The last step must make the TUI quit.
func runQuarkTUI(t *testing.T, home string, steps ...tuiKey) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("drives the TUI through util-linux script(1)")
	}
	scriptPath, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script(1) not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, scriptPath, "-qec",
		"stty cols 140 rows 40; "+binaryPath, "/dev/null")
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "TERM=dumb")
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Start())

	go func() {
		// Wait for the first frame before typing, so keys are not read
		// before the TUI has started.
		time.Sleep(3 * time.Second)
		for _, step := range steps {
			_, _ = io.WriteString(stdin, step.input)
			time.Sleep(step.pause)
		}
		_ = stdin.Close()
	}()
	waitErr := cmd.Wait()
	screen := ansiEscape.ReplaceAllString(out.String(), "")
	if waitErr != nil {
		// Show the end of the last frame so a stuck session is easy to read.
		tail := screen
		if len(tail) > 2500 {
			tail = tail[len(tail)-2500:]
		}
		require.NoError(t, waitErr, "quark TUI must exit after the last step; last output:\n%s", tail)
	}
	return screen
}

// The TUI labels each key the way the platform names it. On Linux the send
// shortcut is shown as Alt+Enter and never as the macOS ⌘+Enter.
func TestCLI_TUIShowsPlatformKeyLabelsForSendShortcut(t *testing.T) {
	home, colID := setupHomeWithCollection(t)
	seedRequest(t, home, colID, &domain.Request{
		Name:   "Fetch",
		Method: http.MethodGet,
		URL:    "http://127.0.0.1:1/fetch",
	})

	screen := runQuarkTUI(t, home, tuiKey{input: "q", pause: 0})

	assert.Contains(t, screen, "Alt+Enter", "the send hint names the Alt key on Linux")
	assert.NotContains(t, screen, "⌘", "Command symbols are only shown on macOS")
}

// Resetting all bindings shows each custom binding with its default label, and
// the confirmation names Enter with the platform label.
func TestCLI_TUIResetAllConfirmationUsesPlatformLabels(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	_, stderr, code := runQuarkWithHome(t, home, "keybindings", "set", "quit", "Q")
	require.Equal(t, 0, code, "keybindings set must succeed: %s", stderr)

	screen := runQuarkTUI(t, home,
		tuiKey{input: "?", pause: time.Second}, // open the keyboard reference
		tuiKey{input: "R", pause: time.Second}, // reset all bindings
		tuiKey{input: "\x1b", pause: time.Second},
		tuiKey{input: "\x1b", pause: time.Second},
		tuiKey{input: "Q", pause: 0}, // quit is bound to Q by the step above
	)

	assert.Contains(t, screen, "Reset all keybindings to defaults?")
	assert.Contains(t, screen, "[Enter] confirm", "Enter is labelled for the platform")
	assert.NotContains(t, screen, "⌘")
}
