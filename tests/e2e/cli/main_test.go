//go:build e2e

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain rebuilds the quark binary when it is missing or older than the
// sources, so `go test -tags e2e ./tests/e2e/cli/...` never runs stale code.
// A binary named by QUARK_BINARY is used as given, and a binary copied from a
// coverage build is newer than the sources, so it is left alone.
func TestMain(m *testing.M) {
	if err := ensureFreshBinary(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: build quark binary:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func ensureFreshBinary() error {
	if os.Getenv("QUARK_BINARY") != "" {
		return nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := filepath.Join(wd, "..", "..", "..")
	info, statErr := os.Stat(binaryPath)
	if statErr == nil {
		newest, err := newestSourceModTime(root)
		if err != nil {
			return err
		}
		if !newest.After(info.ModTime()) {
			return nil
		}
	}
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/quark")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
