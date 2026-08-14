//go:build e2e

package tui_test

import (
	"os"
	"testing"

	"github.com/crazy-vedic/quark/internal/timing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	// QUARK_TIMING is enabled by the Makefile/CI E2E target. Report after all
	// tests so the output contains the aggregate longest operations.
	timing.Default().Report(os.Stdout, 30)
	os.Exit(code)
}
