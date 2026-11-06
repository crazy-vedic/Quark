package updates_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/updates"
)

func TestWingetSource_UpgradeCommand(t *testing.T) {
	source := updates.NewWingetSource("Quark.Quark")
	cmd := source.UpgradeCommand()
	assert.Equal(t, "winget upgrade Quark.Quark", cmd)
}

func TestWingetSource_DowngradeWarning(t *testing.T) {
	source := updates.NewWingetSource("Quark.Quark")
	warning := source.DowngradeWarning("2.0.0", "1.5.0")
	assert.Contains(t, warning, "downgrading")
	assert.Contains(t, warning, "2.0.0")
	assert.Contains(t, warning, "1.5.0")
}

func TestWingetSource_Interface_Compliance(t *testing.T) {
	// Verify WingetSource implements the Source interface
	var _ updates.Source = (*updates.WingetSource)(nil)
}

func TestNewWingetSource(t *testing.T) {
	source := updates.NewWingetSource("Quark.Quark")
	assert.NotNil(t, source)
	assert.NotEmpty(t, source.UpgradeCommand())
}
