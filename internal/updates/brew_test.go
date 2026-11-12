package updates_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/updates"
)

func TestBrewSource_UpgradeCommand(t *testing.T) {
	source := updates.NewBrewSource("crazy-vedic/quark", "quark")
	cmd := source.UpgradeCommand()
	assert.Equal(t, "brew upgrade quark", cmd)
}

func TestBrewSource_DowngradeWarning(t *testing.T) {
	source := updates.NewBrewSource("crazy-vedic/quark", "quark")
	warning := source.DowngradeWarning("2.0.0", "1.5.0")
	assert.Contains(t, warning, "Homebrew does not support pinning")
	assert.Contains(t, warning, "2.0.0")
}

func TestBrewSource_Interface_Compliance(t *testing.T) {
	// Verify BrewSource implements the Source interface
	var _ updates.Source = (*updates.BrewSource)(nil)
}

func TestNewBrewSource(t *testing.T) {
	source := updates.NewBrewSource("crazy-vedic/quark", "quark")
	assert.NotNil(t, source)
	assert.NotEmpty(t, source.UpgradeCommand())
}
