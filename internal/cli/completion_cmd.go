package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCompletionCmd returns a completion command tree with install support.
func NewCompletionCmd(root *cobra.Command) *cobra.Command {
	binary := root.Name()
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate or install shell tab-completion scripts",
		Long: fmt.Sprintf(`Generate or install shell tab-completion for %s.

Quick start:
  %s completion setup          Install for your current shell
  %s completion bash --setup   Install bash completions

To print a script for manual installation:
  %s completion bash`, binary, binary, binary, binary),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newCompletionSetupCmd(root))
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		cmd.AddCommand(newShellCompletionCmd(root, shell))
	}
	return cmd
}

func newCompletionSetupCmd(root *cobra.Command) *cobra.Command {
	var shell string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install shell completions for the current shell",
		Long: fmt.Sprintf(`Install tab-completion for your current shell.

Detects the shell from $SHELL (or $ZSH_VERSION / $FISH_VERSION when set).
Use --shell to override detection.

Example:
  %s completion setup
  %s completion setup --shell zsh`, root.Name(), root.Name()),
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := shell
			if target == "" {
				var err error
				target, err = detectShell()
				if err != nil {
					return err
				}
			}
			return installShellCompletion(root, target, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "Shell to install for (bash, zsh, fish, powershell)")
	return cmd
}

func newShellCompletionCmd(root *cobra.Command, shell string) *cobra.Command {
	var setup bool
	var noDesc bool
	cmd := &cobra.Command{
		Use:   shell,
		Short: fmt.Sprintf("Generate or install the autocompletion script for %s", shell),
		Long:  shellCompletionLong(root.Name(), shell),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if setup {
				return installShellCompletion(root, shell, cmd.ErrOrStderr())
			}
			return writeCompletionScript(root, shell, cmd.OutOrStdout(), noDesc)
		},
	}
	cmd.Flags().BoolVar(&setup, "setup", false, "Install completions for this shell")
	cmd.Flags().BoolVar(&noDesc, "no-descriptions", false, "Disable completion descriptions")
	return cmd
}

func shellCompletionLong(binary, shell string) string {
	switch shell {
	case "bash":
		return fmt.Sprintf(`Generate or install the autocompletion script for bash.

Without --setup, prints the completion script to stdout.

With --setup, writes the script and updates ~/.bashrc automatically.

Manual install:
  source <(%s completion bash)`, binary)
	case "zsh":
		return fmt.Sprintf(`Generate or install the autocompletion script for zsh.

Without --setup, prints the completion script to stdout.

With --setup, writes the script and updates ~/.zshrc automatically.

Manual install:
  source <(%s completion zsh)`, binary)
	case "fish":
		return fmt.Sprintf(`Generate or install the autocompletion script for fish.

Without --setup, prints the completion script to stdout.

With --setup, writes the script to ~/.config/fish/completions/.

Manual install:
  %s completion fish | source`, binary)
	case "powershell":
		return fmt.Sprintf(`Generate or install the autocompletion script for PowerShell.

Without --setup, prints the completion script to stdout.

With --setup, writes the script and updates your PowerShell profile.

Manual install:
  %s completion powershell | Out-String | Invoke-Expression`, binary)
	default:
		return fmt.Sprintf("Generate or install the autocompletion script for %s.", shell)
	}
}
