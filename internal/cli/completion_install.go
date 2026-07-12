package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	completionMarkerStart = "# >>> quark completion >>>"
	completionMarkerEnd   = "# <<< quark completion <<<"

	shellBash       = "bash"
	shellZsh        = "zsh"
	shellFish       = "fish"
	shellPowerShell = "powershell"
	shellPwsh       = "pwsh"
)

func detectShell() (string, error) {
	if os.Getenv("FISH_VERSION") != "" {
		return shellFish, nil
	}
	if os.Getenv("ZSH_VERSION") != "" {
		return shellZsh, nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "", fmt.Errorf(
			"could not detect shell: set SHELL or run quark completion <shell> --setup",
		)
	}
	switch filepath.Base(shell) {
	case shellBash, shellZsh, shellFish, shellPowerShell, shellPwsh:
		name := filepath.Base(shell)
		if name == shellPwsh {
			return shellPowerShell, nil
		}
		return name, nil
	default:
		return "", fmt.Errorf(
			"unsupported shell %q: run quark completion <bash|zsh|fish|powershell> --setup",
			filepath.Base(shell),
		)
	}
}

func writeCompletionScript(root *cobra.Command, shell string, w io.Writer, noDesc bool) error {
	switch shell {
	case shellBash:
		return root.GenBashCompletionV2(w, !noDesc)
	case shellZsh:
		if noDesc {
			return root.GenZshCompletionNoDesc(w)
		}
		return root.GenZshCompletion(w)
	case shellFish:
		return root.GenFishCompletion(w, !noDesc)
	case shellPowerShell:
		if noDesc {
			return root.GenPowerShellCompletion(w)
		}
		return root.GenPowerShellCompletionWithDesc(w)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}

func installShellCompletion(root *cobra.Command, shell string, status io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	binary := root.Name()
	paths, err := completionPaths(shell, home, binary)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(paths.completionFile), 0o755); err != nil {
		return fmt.Errorf("create completion directory: %w", err)
	}

	var script bytes.Buffer
	if err := writeCompletionScript(root, shell, &script, false); err != nil {
		return err
	}
	//nolint:gosec // G306: shell completion scripts must be world-readable
	if err := os.WriteFile(paths.completionFile, script.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write completion script: %w", err)
	}

	if shell == shellZsh {
		clearZshCompdump(home)
	}

	if paths.rcFile == "" {
		fmt.Fprintf(status, "==> Installed %s completions to %s\n", shell, paths.completionFile)
		return nil
	}

	setupBin, err := resolveSetupExecutable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	rcBlock := paths.rcBlock
	if extra := extraPathRegistrationBlock(shell, setupBin, binary); extra != "" {
		rcBlock = injectBeforeEndMarker(rcBlock, extra)
		fmt.Fprintf(
			status,
			"==> Registered completions for %s (%s is not on PATH)\n",
			setupBin,
			binary,
		)
	}

	created, err := appendOrUpdateBlock(paths.rcFile, completionMarkerStart, rcBlock)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(status, "==> Enabled %s completions in %s\n", shell, paths.rcFile)
	} else {
		fmt.Fprintf(status, "==> Updated %s completions at %s\n", shell, paths.completionFile)
	}
	fmt.Fprintf(
		status,
		"    Start a new shell or run: source %s\n",
		paths.rcFile,
	)
	return nil
}

type completionInstallPaths struct {
	completionFile string
	rcFile         string
	rcBlock        string
}

func completionPaths(shell, home, binary string) (completionInstallPaths, error) {
	switch shell {
	case shellBash:
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		completionDir := filepath.Join(dataHome, "bash-completion", "completions")
		completionFile := filepath.Join(completionDir, binary)
		rcFile := filepath.Join(home, ".bashrc")
		block := fmt.Sprintf(`%s
if [ -f %q ]; then
  . %q
fi
%s`, completionMarkerStart, completionFile, completionFile, completionMarkerEnd)
		return completionInstallPaths{
			completionFile: completionFile,
			rcFile:         rcFile,
			rcBlock:        block,
		}, nil
	case shellZsh:
		zdotdir := os.Getenv("ZDOTDIR")
		if zdotdir == "" {
			zdotdir = home
		}
		completionDir := filepath.Join(zdotdir, ".zsh", "completions")
		completionFile := filepath.Join(completionDir, "_"+binary)
		rcFile := filepath.Join(zdotdir, ".zshrc")
		block := fmt.Sprintf(`%s
if [ -d %q ]; then
  fpath=(%q $fpath)
fi
autoload -Uz compinit
compinit
if [ -f %q ]; then
  if (( ${+functions[_%s]} )); then
    unfunction _%s
  fi
  compdef -d %q 2>/dev/null || true
  . %q
  compdef _%s %q 2>/dev/null || true
fi
%s`,
			completionMarkerStart,
			completionDir, completionDir,
			completionFile,
			binary, binary,
			binary,
			completionFile,
			binary, binary,
			completionMarkerEnd,
		)
		return completionInstallPaths{
			completionFile: completionFile,
			rcFile:         rcFile,
			rcBlock:        block,
		}, nil
	case shellFish:
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		completionFile := filepath.Join(configHome, "fish", "completions", binary+".fish")
		return completionInstallPaths{completionFile: completionFile}, nil
	case shellPowerShell:
		completionDir := filepath.Join(home, "Documents", "PowerShell", "Completions")
		completionFile := filepath.Join(completionDir, binary+".ps1")
		profileFile := powershellProfilePath(home)
		block := fmt.Sprintf(`%s
if (Test-Path %q) {
    . %q
}
%s`, completionMarkerStart, completionFile, completionFile, completionMarkerEnd)
		return completionInstallPaths{
			completionFile: completionFile,
			rcFile:         profileFile,
			rcBlock:        block,
		}, nil
	default:
		return completionInstallPaths{}, fmt.Errorf("unsupported shell %q", shell)
	}
}

func resolveSetupExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func binaryOnPathPointsToSelf(selfPath, binary string) bool {
	path, err := exec.LookPath(binary)
	if err != nil {
		return false
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	self, err := filepath.EvalSymlinks(selfPath)
	if err != nil {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(self)
}

func extraPathRegistrationBlock(shell, setupBin, binary string) string {
	if binaryOnPathPointsToSelf(setupBin, binary) {
		return ""
	}
	switch shell {
	case shellBash:
		return fmt.Sprintf(`if [ -x %q ]; then
  complete -o default -F __start_%s %q 2>/dev/null || \
  complete -o default -o nospace -F __start_%s %q 2>/dev/null
fi`, setupBin, binary, setupBin, binary, setupBin)
	case shellZsh:
		return fmt.Sprintf(`if [ -x %q ]; then
  compdef _%s %q 2>/dev/null || true
fi`, setupBin, binary, setupBin)
	default:
		return ""
	}
}

func injectBeforeEndMarker(block, extra string) string {
	extra = strings.TrimRight(extra, "\n")
	if strings.Contains(block, completionMarkerEnd) {
		return strings.Replace(block, completionMarkerEnd, extra+"\n"+completionMarkerEnd, 1)
	}
	return block + "\n" + extra
}

func powershellProfilePath(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "Documents", "PowerShell", "profile.ps1")
	}
	return filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
}

func clearZshCompdump(home string) {
	zdotdir := os.Getenv("ZDOTDIR")
	if zdotdir == "" {
		zdotdir = home
	}
	matches, err := filepath.Glob(filepath.Join(zdotdir, ".zcompdump*"))
	if err != nil {
		return
	}
	for _, match := range matches {
		//nolint:gosec // G703: paths from Glob under known ZDOTDIR/home
		_ = os.Remove(filepath.Clean(match))
	}
}

// appendOrUpdateBlock appends block when marker is absent, or replaces the
// existing marked block. It returns true when a new block was appended.
func appendOrUpdateBlock(targetFile, marker, block string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
		return false, fmt.Errorf("create profile directory: %w", err)
	}

	existing, err := os.ReadFile(targetFile)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", targetFile, err)
	}
	content := string(existing)
	if !strings.Contains(content, marker) {
		var builder strings.Builder
		if len(existing) > 0 && !strings.HasSuffix(content, "\n") {
			builder.WriteByte('\n')
		}
		if existing != nil {
			builder.Write(existing)
		}
		if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") {
			builder.WriteByte('\n')
		}
		builder.WriteString(block)
		builder.WriteByte('\n')
		//nolint:gosec // G306: shell rc/profile files are user-owned and must remain readable
		if err := os.WriteFile(targetFile, []byte(builder.String()), 0o644); err != nil {
			return false, fmt.Errorf("write %s: %w", targetFile, err)
		}
		return true, nil
	}

	endMarker := completionMarkerEnd
	lines := strings.Split(content, "\n")
	var out []string
	inBlock := false
	blockLines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	for _, line := range lines {
		if strings.Contains(line, marker) {
			out = append(out, blockLines...)
			inBlock = true
			continue
		}
		if inBlock {
			if strings.Contains(line, endMarker) {
				inBlock = false
			}
			continue
		}
		out = append(out, line)
	}
	updated := strings.Join(out, "\n")
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	//nolint:gosec // G306: shell rc/profile files are user-owned and must remain readable
	if err := os.WriteFile(targetFile, []byte(updated), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", targetFile, err)
	}
	return false, nil
}
