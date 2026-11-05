package updates

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GitHubSource checks for updates from GitHub Releases.
// This is used for the binary channel only.
type GitHubSource struct {
	repo string // "owner/repo"
}

// NewGitHubSource creates a new GitHub release source.
func NewGitHubSource(repo string) *GitHubSource {
	return &GitHubSource{repo: repo}
}

// LatestVersion gets the latest release version from GitHub Releases.
// Uses redirects, not the REST API, to avoid rate limiting.
func (s *GitHubSource) LatestVersion(ctx context.Context) (string, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects automatically — we'll parse the Location header manually.
			return http.ErrUseLastResponse
		},
	}

	redirectURL := fmt.Sprintf("https://github.com/%s/releases/latest", s.repo)
	req, err := http.NewRequestWithContext(ctx, "GET", redirectURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	// GitHub redirects /releases/latest to /releases/tag/vX.Y.Z
	if resp.StatusCode != http.StatusMovedPermanently &&
		resp.StatusCode != http.StatusFound &&
		resp.StatusCode != http.StatusSeeOther {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header in redirect")
	}

	// Only follow redirects to the same domain
	if !s.isAllowedHost(location) {
		return "", fmt.Errorf("redirect to disallowed host: %s", location)
	}

	// Extract tag from /releases/tag/vX.Y.Z
	parts := strings.Split(location, "/")
	if len(parts) > 0 {
		if tag := parts[len(parts)-1]; strings.HasPrefix(tag, "v") {
			return tag, nil
		}
	}

	return "", fmt.Errorf("could not parse tag from redirect: %s", location)
}

// IsUpdateAvailable checks if a newer version is available.
func (s *GitHubSource) IsUpdateAvailable(
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

// UpgradeCommand returns the command to upgrade via self-update.
func (s *GitHubSource) UpgradeCommand() string {
	return "quark update"
}

// DowngradeWarning returns a message for downgrades.
func (s *GitHubSource) DowngradeWarning(current, target string) string {
	return fmt.Sprintf("downgrading from %s to %s", current, target)
}

// FetchChecksumFile downloads SHA256SUMS for a specific release.
func (s *GitHubSource) FetchChecksumFile(
	ctx context.Context,
	version string,
) (string, error) {
	const checksumFile = "SHA256SUMS"
	const urlTemplate = "https://github.com/%s/releases/download/%s/%s"
	checksumURL := fmt.Sprintf(urlTemplate, s.repo, version, checksumFile)

	req, err := http.NewRequestWithContext(ctx, "GET", checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch SHA256SUMS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SHA256SUMS not found (status %d)", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read SHA256SUMS: %w", err)
	}

	if len(data) == 0 {
		return "", fmt.Errorf("SHA256SUMS is empty")
	}

	return string(data), nil
}

// isAllowedHost checks if a URL is allowed (only github.com).
func (s *GitHubSource) isAllowedHost(location string) bool {
	u, err := url.Parse(location)
	if err != nil {
		return false
	}
	return u.Host == "github.com" || strings.HasSuffix(u.Host, ".github.com")
}
