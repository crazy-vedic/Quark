package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/postman"
	"github.com/crazy-vedic/quark/internal/postmanimport"
)

// ImportPostmanStore is the minimum interface NewImportPostmanCmd requires.
type ImportPostmanStore = postmanimport.Store

// NewImportPostmanCmd returns the 'quark import-postman' subcommand.
func NewImportPostmanCmd(st ImportPostmanStore, logger *DebugLogger) *cobra.Command {
	var collectionName, onDuplicate string
	var includeDisabled bool

	cmd := &cobra.Command{
		Use:   "import-postman <collection.json|directory>",
		Short: "Import a Postman Collection v2.1 JSON file or a bulk export directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := postmanimport.Action(onDuplicate)
			if !action.Valid() {
				logger.Logf("invalid --on-duplicate=%s", onDuplicate)
				return fmt.Errorf(
					"invalid --on-duplicate %q: must be replace, duplicate, merge, or skip",
					onDuplicate,
				)
			}
			opts := postmanimport.Options{
				CollectionName:  collectionName,
				OnDuplicate:     action,
				IncludeDisabled: includeDisabled,
			}
			// Without an explicit --on-duplicate, an interactive terminal is
			// asked about each conflict; otherwise the flag value applies.
			if !cmd.Flags().Changed("on-duplicate") && isTerminal(cmd.InOrStdin()) {
				opts.Resolve = promptDuplicateResolver(cmd, logger)
			}

			result, err := postmanimport.New(st, logger).Import(cmd.Context(), args[0], opts)
			if err != nil {
				return fmt.Errorf("import-postman: %w", err)
			}
			return printPostmanImportResult(cmd, result, logger)
		},
	}

	cmd.Flags().
		StringVarP(&collectionName, "collection-name", "n", "", "Override the imported collection name")
	cmd.Flags().
		BoolVar(&includeDisabled, "include-disabled", false,
			"Import headers that are switched off in Postman as active headers (by default they are left out and listed as warnings)")
	cmd.Flags().
		StringVar(&onDuplicate, "on-duplicate", "duplicate", "Action when collection name already exists: replace, duplicate, merge, skip")

	return cmd
}

// printPostmanImportResult writes the import report and returns an error when
// any environment failed to import.
func printPostmanImportResult(
	cmd *cobra.Command,
	result postmanimport.Result,
	logger *DebugLogger,
) error {
	for _, collection := range result.Collections {
		if collection.Err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "❌ %s: %v\n", collection.FilePath, collection.Err)
			continue
		}
		printImportResult(cmd, collection)
	}

	environments := result.Environments
	if environments.Imported > 0 || len(environments.Errors) > 0 || len(environments.Warnings) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nEnvironments: %d imported", environments.Imported)
		for _, warning := range environments.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "\n  Warning: %s", warning)
		}
		if len(environments.Errors) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), ", %d errors", len(environments.Errors))
			for _, errMsg := range environments.Errors {
				fmt.Fprintf(cmd.ErrOrStderr(), "\n  ⚠️ %s", errMsg)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	summary := result.Summary()
	if len(result.Collections) > 1 {
		fmt.Fprintf(
			cmd.OutOrStdout(),
			"\nSummary: %d imported, %d skipped, %d warnings, %d errors, %d envs across %d collections\n",
			summary.RequestsImported,
			summary.RequestsSkipped,
			summary.Warnings,
			summary.Errors,
			summary.Environments,
			len(result.Collections),
		)
	} else {
		logger.Logf("import complete imported=%d skipped=%d warnings=%d errors=%d envs=%d",
			summary.RequestsImported, summary.RequestsSkipped, summary.Warnings,
			summary.Errors, summary.Environments)
	}
	if len(environments.Errors) > 0 {
		return fmt.Errorf("import-postman: %d environment error(s)", len(environments.Errors))
	}
	return nil
}

func printImportResult(cmd *cobra.Command, s postmanimport.CollectionResult) {
	if s.Err != nil {
		return
	}

	actionLabel := ""
	switch s.Outcome {
	case postmanimport.OutcomeReplaced:
		actionLabel = " (replaced)"
	case postmanimport.OutcomeDuplicated:
		actionLabel = " (duplicated)"
	case postmanimport.OutcomeMerged:
		actionLabel = " (merged)"
	case postmanimport.OutcomeSkipped:
		fmt.Fprintf(cmd.OutOrStdout(), "Skipped %q (%d requests)\n", s.Name, s.Total)
		return
	}

	fmt.Fprintf(cmd.OutOrStdout(),
		"Imported %d/%d requests into collection %q%s",
		s.Imported, s.Total, s.Name, actionLabel)
	if len(s.Warnings) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), " (%d warnings)", len(s.Warnings))
	}
	fmt.Fprintln(cmd.OutOrStdout())

	if s.Security > postman.Safe {
		secLabel := "Review"
		if s.Security == postman.Dangerous {
			secLabel = "Dangerous"
		}
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Security: %s — collection contains potential credentials. Verify before sharing.\n",
			secLabel)
	}

	for _, w := range s.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", w)
	}
}

// promptDuplicateResolver asks on the command's terminal how to handle each
// collection name conflict. A read error falls back to duplicate.
func promptDuplicateResolver(cmd *cobra.Command, logger *DebugLogger) postmanimport.Resolver {
	reader := bufio.NewReader(cmd.InOrStdin())
	return func(_ context.Context, conflict postmanimport.Conflict) (postmanimport.Action, bool, error) {
		return promptDuplicateAction(cmd.OutOrStdout(), reader, conflict, logger)
	}
}

func promptDuplicateAction(
	out io.Writer,
	reader *bufio.Reader,
	conflict postmanimport.Conflict,
	logger *DebugLogger,
) (postmanimport.Action, bool, error) {
	fmt.Fprintf(out,
		"\nCollection %q already exists (%d existing, %d incoming).\n",
		conflict.Name, len(conflict.ExistingRequests), len(conflict.IncomingRequests))

	if len(conflict.ExistingRequests) > 0 {
		fmt.Fprintln(out, "Existing requests:")
		for _, r := range conflict.ExistingRequests {
			fmt.Fprintf(out, "  %s %s\n", r.Method, r.Name)
		}
	}
	if len(conflict.IncomingRequests) > 0 {
		fmt.Fprintln(out, "Incoming requests:")
		for _, r := range conflict.IncomingRequests {
			fmt.Fprintf(out, "  %s %s\n", r.Method, r.Name)
		}
	}

	for {
		fmt.Fprint(out, "[r]eplace, [d]uplicate, [m]erge, [s]kip, [a]pply to all? ")
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Logf("interactive read error, defaulting to duplicate")
			return postmanimport.ActionDuplicate, false, nil
		}
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" {
			continue
		}
		if line == "a" || line == "apply" {
			fmt.Fprint(out, "Apply which action to all? [r]eplace, [d]uplicate, [m]erge, [s]kip? ")
			line2, err := reader.ReadString('\n')
			if err != nil {
				logger.Logf("interactive apply read error, defaulting to duplicate")
				return postmanimport.ActionDuplicate, false, nil
			}
			action, ok := parsePromptAction(line2)
			if !ok {
				fmt.Fprintln(out, "Invalid choice.")
				continue
			}
			logger.Logf("apply-to-all=%s", action)
			return action, true, nil
		}
		if action, ok := parsePromptAction(line); ok {
			logger.Logf("interactive choice=%s", action)
			return action, false, nil
		}
		fmt.Fprintln(out, "Invalid choice. Please enter r, d, m, s, or a.")
	}
}

func parsePromptAction(input string) (postmanimport.Action, bool) {
	switch strings.TrimSpace(strings.ToLower(input)) {
	case "r", "replace":
		return postmanimport.ActionReplace, true
	case "d", "duplicate":
		return postmanimport.ActionDuplicate, true
	case "m", "merge":
		return postmanimport.ActionMerge, true
	case "s", "skip":
		return postmanimport.ActionSkip, true
	}
	return "", false
}

var terminalProbe = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func isTerminal(r io.Reader) bool { return terminalProbe(r) }
