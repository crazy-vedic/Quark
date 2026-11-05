package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// BrewSource checks for updates using Homebrew.
type BrewSource struct {
	tap    string // e.g., "crazy-vedic/quark"
	formula string // e.g., "quark"
}

// NewBrewSource creates a new Homebrew source.
func NewBrewSource(tap, formula string) *BrewSource {
	return &BrewSource{tap: tap, formula: formula}
}

// LatestVersion gets the latest version from the Homebrew tap.
// Uses `brew info --json=v2 <tap>/<formula>`.
func (s *BrewSource) LatestVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "brew", "info", "--json=v2", fmt.Sprintf("%s/%s", s.tap, s.formula))

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("brew info failed: %w", err)
	}

	// Parse the JSON response
	var response struct {
		Formulae []struct {
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
		} `json:"formulae"`
	}

	if err := json.Unmarshal(output, &response); err != nil {
		return "", fmt.Errorf("failed to parse brew output: %w", err)
	}

	if len(response.Formulae) == 0 {
		return "", fmt.Errorf("formula not found in tap")
	}

	version := response.Formulae[0].Versions.Stable
	if version == "" {
		return "", fmt.Errorf("no stable version found")
	}

	// Ensure version has v prefix for consistency
	if version[0] != 'v' {
		version = "v" + version
	}

	return version, nil
}

// IsUpdateAvailable checks if a newer version is available via Homebrew.
func (s *BrewSource) IsUpdateAvailable(ctx context.Context, current string) (bool, string, error) {
	latest, err := s.LatestVersion(ctx)
	if err != nil {
		return false, "", err
	}

	cmp, err := CompareVersions(latest, current)
	if err != nil {
		return false, "", err
	}

	if cmp > 0 {
		return true, latest, nil
	}
	return false, latest, nil
}

// UpgradeCommand returns the Homebrew upgrade command.
func (s *BrewSource) UpgradeCommand() string {
	return "brew upgrade quark"
}

// DowngradeWarning returns a message for downgrades.
func (s *BrewSource) DowngradeWarning(current, target string) string {
	return fmt.Sprintf("Homebrew does not support pinning to specific versions. Current: %s", current)
}
