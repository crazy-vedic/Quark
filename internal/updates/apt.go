package updates

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// AptSource checks for updates using apt.
type AptSource struct {
	packageName string // "quark"
}

// NewAptSource creates a new apt source.
func NewAptSource(packageName string) *AptSource {
	return &AptSource{packageName: packageName}
}

// LatestVersion gets the latest version from apt.
// Uses `apt-cache policy <package>`.
func (s *AptSource) LatestVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "apt-cache", "policy", s.packageName)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("apt-cache policy failed: %w", err)
	}

	// Parse the output for the Candidate line
	// Format:
	//   quark:
	//     Installed: 1.0.0
	//     Candidate: 1.0.1
	// Or:
	//     Candidate: (none)

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Candidate:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				version := parts[1]
				if version == "(none)" {
					return "", fmt.Errorf("package not in apt lists")
				}
				// Ensure version has v prefix for consistency
				if version[0] != 'v' {
					version = "v" + version
				}
				return version, nil
			}
		}
	}

	return "", fmt.Errorf("could not parse apt-cache output")
}

// IsUpdateAvailable checks if a newer version is available via apt.
func (s *AptSource) IsUpdateAvailable(ctx context.Context, current string) (bool, string, error) {
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

// UpgradeCommand returns the apt upgrade command.
func (s *AptSource) UpgradeCommand() string {
	return "sudo apt update && sudo apt upgrade quark"
}

// DowngradeWarning returns a message for downgrades.
func (s *AptSource) DowngradeWarning(current, target string) string {
	return fmt.Sprintf("downgrading from %s to %s", current, target)
}

// ListsAge returns the age of the apt lists in days.
// Returns -1 if the age cannot be determined.
func (s *AptSource) ListsAge() int {
	// The apt lists are typically in /var/lib/apt/lists/
	listDir := "/var/lib/apt/lists"
	entries, err := os.ReadDir(listDir)
	if err != nil || len(entries) == 0 {
		return -1
	}

	// Find the most recent file's modification time
	var newestTime time.Time
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
		}
	}

	if newestTime.IsZero() {
		return -1
	}

	age := time.Since(newestTime)
	return int(age.Hours() / 24)
}

// ParseAptPolicyOutput parses the output from `apt-cache policy`.
// This is a helper for testing.
func ParseAptPolicyOutput(output string) (installed, candidate string, err error) {
	installedRe := regexp.MustCompile(`^\s*Installed:\s*(.+)$`)
	candidateRe := regexp.MustCompile(`^\s*Candidate:\s*(.+)$`)

	for _, line := range strings.Split(output, "\n") {
		if m := installedRe.FindStringSubmatch(line); m != nil {
			installed = strings.TrimSpace(m[1])
		}
		if m := candidateRe.FindStringSubmatch(line); m != nil {
			candidate = strings.TrimSpace(m[1])
		}
	}

	if installed == "" {
		err = fmt.Errorf("installed version not found")
	}
	if candidate == "" {
		err = fmt.Errorf("candidate version not found")
	}

	return installed, candidate, err
}
