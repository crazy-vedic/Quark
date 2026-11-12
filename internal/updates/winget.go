package updates

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// WingetSource checks for updates using Winget.
type WingetSource struct {
	packageID string // "Quark.Quark"
}

// NewWingetSource creates a new Winget source.
func NewWingetSource(packageID string) *WingetSource {
	return &WingetSource{packageID: packageID}
}

// LatestVersion gets the latest version from Winget.
// Uses `winget show --id <package-id> --exact`.
// Runs with English locale forced to ensure consistent output parsing.
func (s *WingetSource) LatestVersion(ctx context.Context) (string, error) {
	args := []string{"show", "--id", s.packageID, "--exact", "--source", "winget"}
	//nolint:gosec
	cmd := exec.CommandContext(ctx, "winget", args...)

	// Force English output for consistent parsing
	cmd.Env = append(os.Environ(), "LANG=en_US.UTF-8")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("winget show failed: %w", err)
	}

	// Parse the output for the Version line
	// Format:
	//   Version                  1.0.1
	version := parseWingetVersion(string(output))
	if version == "" {
		return "", fmt.Errorf("could not parse version from winget output")
	}

	// Ensure version has v prefix for consistency
	if version[0] != 'v' {
		version = "v" + version
	}

	return version, nil
}

// IsUpdateAvailable checks if a newer version is available via Winget.
func (s *WingetSource) IsUpdateAvailable(
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

// UpgradeCommand returns the Winget upgrade command.
func (s *WingetSource) UpgradeCommand() string {
	return fmt.Sprintf("winget upgrade %s", s.packageID)
}

// DowngradeWarning returns a message for downgrades.
func (s *WingetSource) DowngradeWarning(current, target string) string {
	return fmt.Sprintf("downgrading from %s to %s", current, target)
}

// parseWingetVersion parses the version line from winget show output.
// Looks for a line with "Version" and extracts the version number.
func parseWingetVersion(output string) string {
	// Match "Version" or "Version                  1.0.1" etc.
	re := regexp.MustCompile(`^\s*Version\s+(\S+)`)

	for _, line := range strings.Split(output, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}

	return ""
}
