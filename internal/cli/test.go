package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/runner"
)

// NewTestCmd returns the 'quark test' subcommand, which runs every request in
// a collection and reports the results. It exits non-zero when any request
// fails.
func NewTestCmd(st runner.Store, ex runner.Executor) *cobra.Command {
	var format string
	var bail bool

	cmd := &cobra.Command{
		Use:   "test <collection>",
		Short: "Run every request in a collection and report the results",
		Long: `Run every request in a collection, in order: its own requests by sort order,
then each child collection depth first. Each request's extraction rules and
assertions apply, so later requests can use values extracted by earlier ones.

A request fails when an assertion fails or when it gets no response. The
command exits non-zero if any request fails.

Formats: text (default), junit (JUnit XML for CI) and tap (TAP version 13).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case runner.FormatText, runner.FormatJUnit, runner.FormatTAP:
			default:
				return fmt.Errorf("test: unknown format %q (use text, junit or tap)", format)
			}
			results, err := runner.Run(cmd.Context(), st, ex, args[0], runner.Options{Bail: bail})
			if err != nil {
				return fmt.Errorf("test: %w", err)
			}
			if err := runner.WriteReport(cmd.OutOrStdout(), format, results); err != nil {
				return fmt.Errorf("test: write report: %w", err)
			}
			if failed := runner.Failed(results); failed > 0 {
				return fmt.Errorf("test: %d of %d requests failed", failed, len(results))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", runner.FormatText,
		"report format: text, junit or tap")
	cmd.Flags().BoolVar(&bail, "bail", false, "stop after the first failing request")
	return cmd
}
