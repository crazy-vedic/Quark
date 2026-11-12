package updates

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// NpmSource checks for updates using npm registry.
type NpmSource struct {
	packageName string // "@quark-client/quark"
}

// NewNpmSource creates a new npm source.
func NewNpmSource(packageName string) *NpmSource {
	return &NpmSource{packageName: packageName}
}

// LatestVersion gets the latest version from npm registry.
// Uses `npm view <package> version` to query the registry.
func (s *NpmSource) LatestVersion(ctx context.Context) (string, error) {
	//nolint:gosec
	cmd := exec.CommandContext(ctx, "npm", "view", s.packageName, "version", "--json")
	cmd.Env = append(os.Environ(), "LANG=en_US.UTF-8")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("npm view failed: %w", err)
	}

	version := strings.TrimSpace(string(output))
	if version == "" {
		return "", fmt.Errorf("npm view returned empty version")
	}

	// Remove quotes if present (npm --json may include them)
	version = strings.Trim(version, "\"")

	// Ensure version has v prefix for consistency
	if version[0] != 'v' {
		version = "v" + version
	}

	return version, nil
}

// IsUpdateAvailable checks if a newer version is available via npm.
func (s *NpmSource) IsUpdateAvailable(
	ctx context.Context,
	current string,
) (bool, string, error) {
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

// UpgradeCommand returns the npm upgrade command.
func (s *NpmSource) UpgradeCommand() string {
	return fmt.Sprintf("npm i -g %s@latest", s.packageName)
}

// DowngradeWarning returns a message for downgrades.
func (s *NpmSource) DowngradeWarning(current, target string) string {
	return fmt.Sprintf("downgrading from %s to %s", current, target)
}
