package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/updates"
)

func newVersionCmd() *cobra.Command {
	var outputJSON bool
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version and check for updates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// Detect installation channel
			channel := updates.DetectChannel()

			// Create appropriate source based on channel
			var source updates.Source
			switch channel {
			case updates.ChannelBrew:
				source = updates.NewBrewSource("crazy-vedic/quark", "quark")
			case updates.ChannelApt:
				source = updates.NewAptSource("quark")
			case updates.ChannelWinget:
				source = updates.NewWingetSource("Quark.Quark")
			case updates.ChannelBinary:
				source = updates.NewGitHubSource("crazy-vedic/quark")
			default:
				source = updates.NewGitHubSource("crazy-vedic/quark")
			}

			// Check for updates
			available, latest, err := source.IsUpdateAvailable(ctx, Version)

			// Build result
			result := versionResult{
				Current:   Version,
				Latest:    latest,
				Channel:   string(channel),
				Available: available,
			}

			if err == nil {
				result.UpdateCommand = source.UpgradeCommand()
			}

			if outputJSON {
				data, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "quark %s\n", Version)
				if latest != "" && latest != Version {
					fmt.Fprintf(cmd.OutOrStdout(), "latest %s\n", latest)
					if available {
						fmt.Fprintf(cmd.OutOrStdout(), "update available\n")
						if result.UpdateCommand != "" {
							fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", result.UpdateCommand)
						}
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "installed via %s\n", channel)
			}

			// Always exit 0, even if update is available or check fails
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	const checkFlagDesc = "Check for updates without installing (alias for dry-run)"
	cmd.Flags().BoolVar(&checkOnly, "check", false, checkFlagDesc)
	_ = checkOnly // Accepted for compatibility but not used in dry-run mode

	return cmd
}

func newUpdateCmd() *cobra.Command {
	var checkOnly bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "update [version]",
		Short: "Update to the latest version or a specific version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Get target version if provided, otherwise will use latest
			targetVersion := ""
			if len(args) > 0 {
				targetVersion = args[0]
				// Normalize version format (add v prefix if missing)
				if !strings.HasPrefix(targetVersion, "v") {
					targetVersion = "v" + targetVersion
				}
			}

			// Get current executable path
			exePath, err := os.Executable()
			if err != nil {
				return err
			}

			// Detect installation channel
			channel := updates.DetectChannel()

			if checkOnly {
				// Dry-run mode: show what would happen
				switch channel {
				case updates.ChannelBrew:
					source := updates.NewBrewSource("crazy-vedic/quark", "quark")
					available, latest, err := source.IsUpdateAvailable(ctx, Version)
					switch {
					case err != nil:
						fmt.Fprintf(cmd.OutOrStderr(), "Could not check for updates: %v\n", err)
					case available:
						fmt.Fprintf(cmd.OutOrStderr(), "Update available: %s\n", latest)
						fmt.Fprintf(cmd.OutOrStderr(), "Command: %s\n", source.UpgradeCommand())
					default:
						fmt.Fprintf(cmd.OutOrStderr(), "Already up to date\n")
					}
					return nil

				case updates.ChannelApt:
					source := updates.NewAptSource("quark")
					available, latest, err := source.IsUpdateAvailable(ctx, Version)
					switch {
					case err != nil:
						fmt.Fprintf(cmd.OutOrStderr(), "Could not check for updates: %v\n", err)
					case available:
						fmt.Fprintf(cmd.OutOrStderr(), "Update available: %s\n", latest)
						fmt.Fprintf(cmd.OutOrStderr(), "Command: %s\n", source.UpgradeCommand())
					default:
						fmt.Fprintf(cmd.OutOrStderr(), "Already up to date\n")
					}
					return nil

				case updates.ChannelWinget:
					source := updates.NewWingetSource("Quark.Quark")
					available, latest, err := source.IsUpdateAvailable(ctx, Version)
					switch {
					case err != nil:
						fmt.Fprintf(cmd.OutOrStderr(), "Could not check for updates: %v\n", err)
					case available:
						fmt.Fprintf(cmd.OutOrStderr(), "Update available: %s\n", latest)
						fmt.Fprintf(cmd.OutOrStderr(), "Command: %s\n", source.UpgradeCommand())
					default:
						fmt.Fprintf(cmd.OutOrStderr(), "Already up to date\n")
					}
					return nil

				case updates.ChannelBinary:
					// For binary, show what self-update would do
					source := updates.NewGitHubSource("crazy-vedic/quark")
					available, latest, err := source.IsUpdateAvailable(ctx, Version)
					switch {
					case err != nil:
						fmt.Fprintf(cmd.OutOrStderr(), "Could not check for updates: %v\n", err)
					case available:
						fmt.Fprintf(cmd.OutOrStderr(), "Update available: %s\n", latest)
						fmt.Fprintf(cmd.OutOrStderr(), "Run: quark update (without --check)\n")
					default:
						fmt.Fprintf(cmd.OutOrStderr(), "Already up to date\n")
					}
					return nil
				}
			}

			// Only allow self-update for binary channel
			if channel != updates.ChannelBinary {
				fmt.Fprintf(cmd.OutOrStderr(), "Self-update only works for binary installations\n")
				fmt.Fprintf(cmd.OutOrStderr(), "You are using the %s channel\n", channel)

				switch channel {
				case updates.ChannelBrew:
					brewSource := updates.NewBrewSource("crazy-vedic/quark", "quark")
					fmt.Fprintf(cmd.OutOrStderr(), "To upgrade: %s\n", brewSource.UpgradeCommand())
				case updates.ChannelApt:
					aptSource := updates.NewAptSource("quark")
					fmt.Fprintf(cmd.OutOrStderr(), "To upgrade: %s\n", aptSource.UpgradeCommand())
				case updates.ChannelWinget:
					wingetSource := updates.NewWingetSource("Quark.Quark")
					upgrade := wingetSource.UpgradeCommand()
					fmt.Fprintf(cmd.OutOrStderr(), "To upgrade: %s\n", upgrade)
				}

				return nil
			}

			// Check if self-update is allowed
			if !updates.CanSelfUpdate(channel, exePath) {
				fmt.Fprintf(cmd.OutOrStderr(), "Self-update not allowed from system paths\n")
				fmt.Fprint(cmd.OutOrStderr(),
					"Please reinstall using: brew install quark (or apt/winget)\n")
				return nil
			}

			// Perform self-update
			updater := updates.NewSelfUpdater("crazy-vedic/quark", exePath, Version)

			var updateToVersion string
			if targetVersion != "" {
				// User specified a target version
				updateToVersion = targetVersion
			} else {
				// Check for latest version
				available, latestVersion, err := updater.CheckUpdate(ctx)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "Could not check for updates: %v\n", err)
					return nil
				}

				if !available {
					fmt.Fprintf(cmd.OutOrStderr(), "Already up to date (%s)\n", Version)
					return nil
				}
				updateToVersion = latestVersion
			}

			// Ask for confirmation unless --yes is set
			if !yes {
				fmt.Fprintf(cmd.OutOrStderr(), "Update available: %s → %s\n", Version, updateToVersion)
				fmt.Fprint(cmd.OutOrStderr(), "Proceed with update? [y/N] ")
				var reply string
				if _, err := fmt.Scanln(&reply); err != nil {
					fmt.Fprintf(cmd.OutOrStderr(), "Aborted\n")
					return nil
				}
				reply = strings.ToLower(strings.TrimSpace(reply))
				if reply != "y" && reply != "yes" {
					fmt.Fprintf(cmd.OutOrStderr(), "Aborted\n")
					return nil
				}
			}

			fmt.Fprintf(cmd.OutOrStderr(), "Updating from %s to %s...\n", Version, updateToVersion)
			newVersion, err := updater.Update(ctx, updateToVersion)
			if err != nil {
				fmt.Fprintf(cmd.OutOrStderr(), "Update failed: %v\n", err)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStderr(), "Successfully updated to %s\n", newVersion)
			return nil
		},
	}

	const dryRunDesc = "Check for updates without installing (dry-run mode)"
	cmd.Flags().BoolVar(&checkOnly, "check", false, dryRunDesc)
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt and proceed with update")

	return cmd
}

// versionResult is the JSON output structure for the version command.
type versionResult struct {
	Current       string `json:"current"`
	Latest        string `json:"latest,omitempty"`
	Channel       string `json:"channel"`
	Available     bool   `json:"available"`
	UpdateCommand string `json:"update_command,omitempty"`
}
