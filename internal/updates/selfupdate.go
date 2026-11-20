package updates

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SelfUpdater handles binary self-update operations.
type SelfUpdater struct {
	gh             *GitHubSource
	currentPath    string
	currentVersion string
}

// NewSelfUpdater creates a new self-updater for the binary channel.
func NewSelfUpdater(repo string, currentPath string, currentVersion string) *SelfUpdater {
	return &SelfUpdater{
		gh:             NewGitHubSource(repo),
		currentPath:    currentPath,
		currentVersion: currentVersion,
	}
}

// SetBaseURL overrides the host release assets are downloaded from. The
// default is https://github.com; tests point it at a local server.
func (su *SelfUpdater) SetBaseURL(baseURL string) {
	su.gh.baseURL = strings.TrimRight(baseURL, "/")
}

// CheckUpdate checks if an update is available without downloading.
func (su *SelfUpdater) CheckUpdate(ctx context.Context) (bool, string, error) {
	return su.gh.IsUpdateAvailable(ctx, su.currentVersion)
}

// Update downloads, verifies, and installs the new binary.
// Returns the new version on success, or an error on failure (with rollback if needed).
func (su *SelfUpdater) Update(ctx context.Context, targetVersion string) (string, error) {
	// Build binary name based on current OS and architecture
	binaryName := su.getBinaryName()

	// Download to temporary file
	tmpFile, err := su.downloadBinary(ctx, targetVersion, binaryName)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpFile)

	// Verify checksum
	if err := su.verifyChecksum(ctx, targetVersion, binaryName, tmpFile); err != nil {
		return "", fmt.Errorf("checksum verification failed: %w", err)
	}

	// Make backup of current binary
	backupPath := su.currentPath + ".prev"
	if err := su.backup(su.currentPath, backupPath); err != nil {
		return "", fmt.Errorf("backup failed: %w", err)
	}

	// Replace binary
	if err := su.replace(tmpFile, su.currentPath); err != nil {
		// Attempt rollback
		if rollbackErr := su.restore(backupPath, su.currentPath); rollbackErr != nil {
			return "", fmt.Errorf("replace failed: %w, and rollback failed: %w", err, rollbackErr)
		}
		return "", fmt.Errorf("replace failed (rolled back): %w", err)
	}

	// Verify post-install by running new binary with --version
	if err := su.verifyPostInstall(ctx); err != nil {
		// Attempt rollback
		if rollbackErr := su.restore(backupPath, su.currentPath); rollbackErr != nil {
			msg := "post-install check failed: %w, and rollback failed: %w"
			return "", fmt.Errorf(msg, err, rollbackErr)
		}
		return "", fmt.Errorf("post-install check failed (rolled back): %w", err)
	}

	// Clean up backup on Windows by renaming to .old for lazy cleanup
	if runtime.GOOS == "windows" && backupPath != "" {
		oldPath := su.currentPath + ".old"
		os.Remove(oldPath) // Try to remove stale .old from previous update
		if err := os.Rename(backupPath, oldPath); err != nil {
			return "", fmt.Errorf("rename backup to .old: %w", err)
		}
	} else {
		os.Remove(backupPath)
	}

	return targetVersion, nil
}

// SetHTTPTimeout changes the per-request timeout. Tests use it.
func (su *SelfUpdater) SetHTTPTimeout(d time.Duration) {
	su.gh.SetHTTPTimeout(d)
}

// getBinaryName constructs the binary filename for the current platform.
func (su *SelfUpdater) getBinaryName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	base := fmt.Sprintf("quark-%s-%s", goos, goarch)
	if goos == "windows" {
		return base + ".exe"
	}
	return base
}

// downloadBinary downloads the binary from the GitHub release.
func (su *SelfUpdater) downloadBinary(
	ctx context.Context,
	version, binaryName string,
) (string, error) {
	downloadURL := fmt.Sprintf("%s/%s/releases/download/%s/%s",
		su.gh.baseURL, su.gh.repo, version, binaryName)

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := (&http.Client{Timeout: su.gh.timeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Prefer a temp file beside the binary so the final rename stays on one
	// filesystem; fall back to the system temp dir when that is not writable.
	tmp, err := os.CreateTemp(filepath.Dir(su.currentPath), ".quark-update-*.tmp")
	if err != nil {
		tmp, err = os.CreateTemp("", "quark-update-*.tmp")
		if err != nil {
			return "", err
		}
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	// os.CreateTemp creates the file 0600. The file replaces the running
	// binary, so it needs the execute bits of the binary it replaces.
	if err := os.Chmod(tmpPath, su.installMode()); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	return tmpPath, nil
}

// installMode returns the permissions for the new binary: those of the
// current binary with the execute bits set, or 0755 when it cannot be read.
func (su *SelfUpdater) installMode() os.FileMode {
	info, err := os.Stat(su.currentPath)
	if err != nil {
		return 0o755
	}
	return info.Mode().Perm() | 0o111
}

// verifyChecksum downloads SHA256SUMS and verifies the binary.
func (su *SelfUpdater) verifyChecksum(
	ctx context.Context,
	version, binaryName, filePath string,
) error {
	checksumData, err := su.gh.FetchChecksumFile(ctx, version)
	if err != nil {
		return fmt.Errorf("failed to fetch SHA256SUMS: %w", err)
	}

	// Parse checksumData for the binary's hash
	expected := su.parseChecksumLine(checksumData, binaryName)
	if expected == "" {
		return fmt.Errorf("checksum for %s not found in SHA256SUMS", binaryName)
	}

	// Compute hash of the downloaded file
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

// parseChecksumLine finds the hash line for the given filename.
// Format: "hash  filename"
func (su *SelfUpdater) parseChecksumLine(checksumData, filename string) string {
	for _, line := range strings.Split(checksumData, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == filename {
			return parts[0]
		}
	}
	return ""
}

// backup creates a backup of the current binary.
func (su *SelfUpdater) backup(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Preserve executable permissions
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

// replace moves the new binary into place.
// On Unix, this is a simple atomic rename.
// On Windows, the old file must be closed first, so we use rename-aside strategy.
func (su *SelfUpdater) replace(src, dst string) error {
	if runtime.GOOS == "windows" {
		// On Windows, we can't replace a running executable, so move it aside first
		aside := dst + ".aside"
		if err := os.Rename(dst, aside); err != nil {
			return fmt.Errorf("failed to move aside current binary: %w", err)
		}
		// Now rename new binary into place
		if err := os.Rename(src, dst); err != nil {
			// Try to restore original binary
			if restoreErr := os.Rename(aside, dst); restoreErr != nil {
				msg := "replace failed and restoration failed: replace=%w, restore=%w"
				return fmt.Errorf(msg, err, restoreErr)
			}
			return fmt.Errorf("failed to rename new binary into place: %w", err)
		}
		// Clean up the aside copy (best effort)
		os.Remove(aside)
		return nil
	}

	// On Unix, atomic rename is safe
	return os.Rename(src, dst)
}

// restore rolls back to the backup binary.
func (su *SelfUpdater) restore(backupPath, dst string) error {
	backupFile, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer backupFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, backupFile); err != nil {
		return err
	}

	// Restore executable permissions
	info, err := os.Stat(backupPath)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

// verifyPostInstall runs the new binary to verify it works.
func (su *SelfUpdater) verifyPostInstall(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, su.currentPath, "--version") //nolint:gosec
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("post-install check failed: %w (output: %s)", err, string(output))
	}
	return nil
}
