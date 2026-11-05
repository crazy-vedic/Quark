package updates

import (
	"context"
	"time"
)

// Source is the interface for checking updates from a package manager or release service.
type Source interface {
	// LatestVersion returns the latest available version.
	// Returns empty string if unavailable.
	LatestVersion(ctx context.Context) (string, error)

	// IsUpdateAvailable checks if a newer version is available.
	// Compares using semver rules.
	IsUpdateAvailable(ctx context.Context, current string) (bool, string, error)

	// UpgradeCommand returns the shell command to upgrade.
	UpgradeCommand() string

	// DowngradeWarning returns a warning message if a downgrade is requested.
	DowngradeWarning(current, target string) string
}

// VersionCheckResult holds the result of a version check.
type VersionCheckResult struct {
	Channel              Channel
	Current              string
	Latest               string
	UpdateAvailable      bool
	UpgradeCommand       string
	CanSelfUpdate        bool
	CheckedAt            time.Time
	Error                error
	ListsAgeDays         int // For apt only
	DowngradeWarning     string
}

// Result strings for display.
func (r *VersionCheckResult) StatusMessage() string {
	if r.Error != nil {
		return "update check failed: " + r.Error.Error()
	}
	if !r.UpdateAvailable {
		return "up to date (via " + string(r.Channel) + ")"
	}
	if r.CanSelfUpdate {
		return "update available: " + r.Latest + " (run: quark update)"
	}
	return "update available: " + r.Latest + " (run: " + r.UpgradeCommand + ")"
}

// StatusBarText returns a brief status bar representation.
func (r *VersionCheckResult) StatusBarText() string {
	if !r.UpdateAvailable {
		return ""
	}
	if r.CanSelfUpdate {
		return "↑ " + r.Latest + " · quark update"
	}
	return "↑ " + r.Latest + " · " + r.UpgradeCommand
}
