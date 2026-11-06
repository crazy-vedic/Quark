package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// NewKeybindingsCmd returns the 'quark keybindings' subcommand.
// configDir is the directory where config.toml and the DB are stored.
func NewKeybindingsCmd(configDir string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keybindings",
		Short: "Manage keybindings",
	}
	cmd.AddCommand(newKeybindingsListCmd(configDir))
	cmd.AddCommand(newKeybindingsSetCmd(configDir))
	cmd.AddCommand(newKeybindingsResetCmd(configDir))
	return cmd
}

func newKeybindingsListCmd(configDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show current keybindings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configDir)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			entries := keybindings.ListEntries(cfg.Keybindings)
			var lastGroup string
			for _, e := range entries {
				if e.Group != lastGroup {
					fmt.Fprintf(cmd.OutOrStdout(), "\n[%s]\n", e.Group)
					lastGroup = e.Group
				}
				key := e.Key
				if key == "" {
					key = "(unbound)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %s\n", e.Action, key)
			}

			return nil
		},
	}
}

func newKeybindingsSetCmd(configDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "set <action> <key>",
		Short: "Set a keybinding (e.g. 'quark keybindings set quit q')",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			action, key := args[0], args[1]

			cfg, err := config.Load(configDir)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Validate action exists.
			old := keybindings.GetAction(cfg.Keybindings, action)
			if old == "" {
				return fmt.Errorf("unknown action: %s", action)
			}

			// Validate and apply.
			newBinds, err := keybindings.RecordBinding(cfg.Keybindings, action, key)
			if err != nil {
				return fmt.Errorf("conflict detected: %w", err)
			}

			// Save.
			if err := config.SaveKeybindings(configDir, newBinds); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s (was %s)\n", action, key, old)
			return nil
		},
	}
}

func newKeybindingsResetCmd(configDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset all keybindings to defaults",
		RunE: func(cmd *cobra.Command, _ []string) error {
			binds := keybindings.DefaultKeybindings()
			if err := config.SaveKeybindings(configDir, binds); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Keybindings reset to defaults.")
			return nil
		},
	}
}
