package updates

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// CompareVersions compares two version strings using semver rules.
// Returns:
//   - positive if v1 > v2
//   - negative if v1 < v2
//   - zero if v1 == v2
func CompareVersions(v1, v2 string) (int, error) {
	sv1, err := semver.NewVersion(v1)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", v1, err)
	}
	sv2, err := semver.NewVersion(v2)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q: %w", v2, err)
	}
	return sv1.Compare(sv2), nil
}

// IsPrerelease checks if a version string represents a pre-release.
func IsPrerelease(v string) bool {
	sv, err := semver.NewVersion(v)
	if err != nil {
		return false
	}
	return sv.Prerelease() != ""
}
