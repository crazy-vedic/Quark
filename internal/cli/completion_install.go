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

	"github.com/crazy-vedic/quark/internal/atomicfile"
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
	if err := atomicfile.WriteFile(paths.completionFile, script.Bytes(), 0o644); err != nil {
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
	if shell == shellZsh {
		fmt.Fprintf(
			status,
			"    zsh completions need compinit; if %s does not run it, add: "+
				"autoload -Uz compinit && compinit\n",
			paths.rcFile,
		)
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
		// Only extend fpath: the user's own compinit (before or after this
		// block) picks the script up. When compinit already ran, register
		// the function directly instead of running compinit a second time.
		block := fmt.Sprintf(`%s
fpath=(%q $fpath)
if (( ${+functions[compdef]} )); then
  autoload -Uz _%s
  compdef _%s %s
fi
%s`,
			completionMarkerStart,
			completionDir,
			binary,
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

// clearZshCompdump removes this host's zsh completion cache so the next
// compinit rebuilds it. Only the default dump (.zcompdump) and the per-host
// dumps that frameworks such as oh-my-zsh write (.zcompdump-<host>-<version>)
// are removed; dumps of other hosts sharing the home directory are kept.
func clearZshCompdump(home string) {
	zdotdir := os.Getenv("ZDOTDIR")
	if zdotdir == "" {
		zdotdir = home
	}
	targets := []string{
		filepath.Join(zdotdir, ".zcompdump"),
		filepath.Join(zdotdir, ".zcompdump.zwc"),
	}
	if host := shortHostname(); host != "" {
		matches, err := filepath.Glob(filepath.Join(zdotdir, ".zcompdump-"+host+"-*"))
		if err == nil {
			targets = append(targets, matches...)
		}
	}
	for _, target := range targets {
		//nolint:gosec // G703: paths under known ZDOTDIR/home
		_ = os.Remove(filepath.Clean(target))
	}
}

// shortHostname returns the host name up to the first dot, like zsh's
// ${HOST%%.*}. It returns "" when the host name is unknown or contains glob
// metacharacters.
func shortHostname() string {
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	host, _, _ = strings.Cut(host, ".")
	if strings.ContainsAny(host, `*?[\/`) {
		return ""
	}
	return host
}

// uninstallShellCompletion removes the marked block from the shell rc or
// profile file and deletes the installed completion script.
func uninstallShellCompletion(root *cobra.Command, shell string, status io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	paths, err := completionPaths(shell, home, root.Name())
	if err != nil {
		return err
	}

	removedBlock := false
	if paths.rcFile != "" {
		removedBlock, err = removeMarkedBlock(paths.rcFile)
		if err != nil {
			return err
		}
	}
	removedScript := false
	if err := os.Remove(paths.completionFile); err == nil {
		removedScript = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("remove completion script: %w", err)
	}
	if shell == shellZsh && removedScript {
		clearZshCompdump(home)
	}

	if !removedBlock && !removedScript {
		fmt.Fprintf(status, "==> No %s completions installed\n", shell)
		return nil
	}
	fmt.Fprintf(status, "==> Removed %s completions\n", shell)
	if removedScript {
		fmt.Fprintf(status, "    Deleted %s\n", paths.completionFile)
	}
	if removedBlock {
		fmt.Fprintf(status, "    Removed the completion block from %s\n", paths.rcFile)
	}
	return nil
}

// removeMarkedBlock deletes the lines from completionMarkerStart through
// completionMarkerEnd in targetFile. It returns false when the file or the
// block does not exist.
func removeMarkedBlock(targetFile string) (bool, error) {
	existing, err := os.ReadFile(targetFile)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", targetFile, err)
	}
	content := string(existing)
	if !strings.Contains(content, completionMarkerStart) {
		return false, nil
	}
	var out []string
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.Contains(line, completionMarkerStart):
			inBlock = true
		case inBlock:
			if strings.Contains(line, completionMarkerEnd) {
				inBlock = false
			}
		default:
			out = append(out, line)
		}
	}
	updated := strings.Join(out, "\n")
	mode := rcFileMode(targetFile)
	if err := atomicfile.WriteFile(targetFile, []byte(updated), mode); err != nil {
		return false, fmt.Errorf("write %s: %w", targetFile, err)
	}
	return true, nil
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
		mode := rcFileMode(targetFile)
		if err := atomicfile.WriteFile(targetFile, []byte(builder.String()), mode); err != nil {
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
	mode := rcFileMode(targetFile)
	if err := atomicfile.WriteFile(targetFile, []byte(updated), mode); err != nil {
		return false, fmt.Errorf("write %s: %w", targetFile, err)
	}
	return false, nil
}

// rcFileMode keeps the mode of an existing shell rc/profile file, so editing
// it never loosens or tightens permissions; a new file is 0644.
func rcFileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}
