package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/theme"
)

// Theme commands intentionally avoid runtimeOnce: inspecting and editing
// appearance must not open the request database or initialize HTTP transport.
func newThemeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "theme", Short: "List, validate, or select terminal themes"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List built-in and local themes", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			names, err := theme.List(configDir)
			if err != nil {
				return err
			}
			cfg, err := config.Load(configDir)
			if err != nil {
				return err
			}
			for _, name := range names {
				marker := "  "
				if name == cfg.UI.Theme {
					marker = "* "
				}
				fmt.Fprintln(cmd.OutOrStdout(), marker+name)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "set NAME", Short: "Save the selected theme to config.toml", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := theme.Resolve(configDir, args[0], lipgloss.HasDarkBackground()); err != nil {
				return err
			}
			if err := config.SaveTheme(configDir, args[0]); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Theme saved:", args[0])
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "validate [NAME]", Short: "Validate a theme file or the current selection", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else {
				cfg, err := config.Load(configDir)
				if err != nil {
					return err
				}
				name = cfg.UI.Theme
				if value := os.Getenv("QUARK_THEME"); value != "" {
					name = value
				}
				if themeFlag != "" {
					name = themeFlag
				}
			}
			resolved, err := theme.Resolve(configDir, name, lipgloss.HasDarkBackground())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Theme %s is valid (%s palette)\n", resolved.Name, resolved.Variant)
			return nil
		},
	})
	return cmd
}
