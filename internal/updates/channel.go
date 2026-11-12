package updates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Channel represents the installation channel for Quark.
type Channel string

const (
	ChannelBrew   Channel = "brew"
	ChannelApt    Channel = "apt"
	ChannelWinget Channel = "winget"
	ChannelNpm    Channel = "npm"
	ChannelBinary Channel = "binary"
)

// DetectChannel determines the installation channel based on the resolved executable path.
// It uses runtime detection, not build-time stamps, so every binary can work with any channel.
func DetectChannel() Channel {
	exe, err := os.Executable()
	if err != nil {
		return ChannelBinary
	}

	// Resolve symlinks and relative paths to get the actual binary location.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return ChannelBinary
	}

	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return ChannelBinary
	}

	// Check Homebrew: /usr/local/Cellar or /opt/homebrew/Cellar
	if strings.Contains(resolved, "/Cellar/quark/") {
		if commandExists("brew") {
			return ChannelBrew
		}
	}

	// Check apt: /usr/bin/quark with dpkg ownership
	if resolved == "/usr/bin/quark" {
		if commandExists("dpkg-query") {
			cmd := exec.Command("dpkg-query", "-S", "/usr/bin/quark")
			if err := cmd.Run(); err == nil {
				return ChannelApt
			}
		}
	}

	// Check Winget: %LOCALAPPDATA%\Microsoft\WinGet\Packages\Quark.Quark_*
	home, err := os.UserHomeDir()
	if err == nil {
		wingetPath := filepath.Join(home, "AppData", "Local", "Microsoft", "WinGet", "Packages")
		if strings.HasPrefix(resolved, wingetPath) && strings.Contains(resolved, "Quark.Quark") {
			if commandExists("winget") {
				return ChannelWinget
			}
		}
	}

	// Check npm: node_modules/@quark-client/quark/*/bin or global npm prefix
	// Also skip npx cache paths (_npx) — npx always runs a fresh copy.
	if strings.Contains(resolved, "node_modules") || strings.Contains(resolved, "_npx") {
		if strings.Contains(resolved, "@quark-client") && strings.Contains(resolved, "quark") {
			if commandExists("npm") {
				if strings.Contains(resolved, "_npx") {
					// npx cache — still npm, but don't show upgrade notices
					return ChannelNpm
				}
				return ChannelNpm
			}
		}
	}

	// Default to binary for anything else.
	return ChannelBinary
}

// CanSelfUpdate returns whether this binary can safely self-update from this channel.
// Binary channel can self-update, but only from user-writable, non-system paths.
// Package-managed channels never self-update.
func CanSelfUpdate(ch Channel, exe string) bool {
	if ch != ChannelBinary {
		return false
	}

	// Resolve the executable path
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return false
	}

	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return false
	}

	// Check if the path is in a system package directory (never self-update there).
	// Note: /usr/local/bin is user-writable, so it's allowed for self-update.
	systemPaths := []string{
		"/usr/bin",
		"/usr/local/Cellar",
		"/opt/homebrew",
		"/opt/homebrew/bin",
		"C:\\Program Files",
	}

	for _, sys := range systemPaths {
		// Check exact path or exact prefix with separator (not just string prefix)
		// This prevents false matches like "/usr/binfoo" matching "/usr/bin"
		if resolved == sys || strings.HasPrefix(resolved, sys+"/") {
			return false
		}
	}

	// Check if the directory is writable by the user.
	dir := filepath.Dir(resolved)
	return isWritable(dir)
}

// isWritable checks if the directory is writable by the current user.
func isWritable(dir string) bool {
	// Try creating a temporary file in the directory.
	f, err := os.CreateTemp(dir, ".quark-check-*")
	if err != nil {
		return false
	}
	defer f.Close()
	defer os.Remove(f.Name())
	return true
}

// commandExists checks if a command is available on PATH.
func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}
