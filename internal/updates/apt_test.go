package updates_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/updates"
)

func TestAptSource_UpgradeCommand(t *testing.T) {
	source := updates.NewAptSource("quark")
	cmd := source.UpgradeCommand()
	assert.Equal(t, "sudo apt update && sudo apt upgrade quark", cmd)
}

func TestAptSource_DowngradeWarning(t *testing.T) {
	source := updates.NewAptSource("quark")
	warning := source.DowngradeWarning("2.0.0", "1.5.0")
	assert.Contains(t, warning, "downgrading")
	assert.Contains(t, warning, "2.0.0")
	assert.Contains(t, warning, "1.5.0")
}

func TestAptSource_Interface_Compliance(t *testing.T) {
	// Verify AptSource implements the Source interface
	var _ updates.Source = (*updates.AptSource)(nil)
}

func TestNewAptSource(t *testing.T) {
	source := updates.NewAptSource("quark")
	assert.NotNil(t, source)
	assert.NotEmpty(t, source.UpgradeCommand())
}
