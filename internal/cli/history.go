package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// HistoryStore is the minimum interface NewHistoryCmd requires.
type HistoryStore interface {
	store.CollectionLister
	store.RequestReader
	store.ExecutionReader
}

// NewHistoryCmd returns the 'quark history' subcommand. It lists the recorded
// runs of a request, newest first, or prints one run when an execution ID (or
// a unique prefix of one) is given.
func NewHistoryCmd(st HistoryStore) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "history <Collection/Request> [execution-id]",
		Short: "List or show a request's execution history",
		Long: "List the recorded runs of a request, newest first.\n\n" +
			"Pass an execution ID, or a unique prefix of one, to print that run " +
			"with its status, headers and stored body.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			req, err := resolveHistoryRequest(ctx, st, args[0])
			if err != nil {
				return fmt.Errorf("history: resolve request: %w", err)
			}
			if len(args) == 2 {
				return showHistoryEntry(ctx, cmd.OutOrStdout(), st, req.ID, args[1])
			}
			return listHistory(ctx, cmd.OutOrStdout(), st, req.ID, limit)
		},
	}
	cmd.ValidArgsFunction = completeHistoryArgs(st)
	cmd.Flags().IntVar(&limit, "limit", 20,
		"Maximum number of runs to list (0 lists every run)")
	return cmd
}

// resolveHistoryRequest resolves a request path the way `quark run` does.
func resolveHistoryRequest(
	ctx context.Context,
	st HistoryStore,
	path string,
) (*domain.Request, error) {
	if resolver, ok := any(st).(requestPathResolver); ok {
		return resolver.ResolveRequestPath(ctx, path)
	}
	req, err := findRequestByPath(ctx, st, path)
	return req, err
}

func listHistory(
	ctx context.Context,
	out io.Writer,
	st HistoryStore,
	requestID string,
	limit int,
) error {
	runs, err := st.ListExecutionsByRequest(ctx, requestID, limit)
	if err != nil {
		return fmt.Errorf("history: list runs: %w", err)
	}
	if len(runs) == 0 {
		fmt.Fprintln(out, "No history found.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tCOMPLETED\tSTATUS\tTIME\tSIZE\tERROR")
	for _, ex := range runs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%dms\t%d bytes\t%s\n",
			ex.ID,
			ex.CompletedAt.Local().Format(time.DateTime),
			statusLabel(ex.StatusCode),
			ex.ResponseTimeMs,
			ex.BodySize(),
			oneLine(safeText(out, ex.Error)),
		)
	}
	return tw.Flush()
}

func showHistoryEntry(
	ctx context.Context,
	out io.Writer,
	st HistoryStore,
	requestID, ref string,
) error {
	runs, err := st.ListExecutionsByRequest(ctx, requestID, 0)
	if err != nil {
		return fmt.Errorf("history: list runs: %w", err)
	}
	match, err := matchExecution(runs, ref)
	if err != nil {
		return err
	}
	ex, err := st.GetExecution(ctx, match.ID)
	if err != nil {
		return fmt.Errorf("history: load run: %w", err)
	}

	fmt.Fprintf(out, "Run:       %s\n", ex.ID)
	fmt.Fprintf(out, "Completed: %s\n", ex.CompletedAt.Local().Format(time.DateTime))
	fmt.Fprintf(out, "Status:    %s\n", statusLabel(ex.StatusCode))
	fmt.Fprintf(out, "Time:      %dms\n", ex.ResponseTimeMs)
	fmt.Fprintf(out, "Size:      %d bytes\n", ex.BodySize())
	if ex.ResponseBodyTruncated {
		fmt.Fprintf(out, "Body:      stored first %d of %d bytes\n",
			len(ex.ResponseBody), ex.ResponseSize)
	}
	if ex.Error != "" {
		fmt.Fprintf(out, "Error:     %s\n", oneLine(safeText(out, ex.Error)))
	}

	headers := storedHeaders(ex.ResponseHeaders)
	if len(headers) > 0 {
		fmt.Fprintln(out, "Headers:")
		keys := make([]string, 0, len(headers))
		for key := range headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(out, "  %s: %s\n", safeText(out, key),
				safeText(out, strings.Join(headers[key], ", ")))
		}
	}

	if ex.ResponseBody == "" {
		return nil
	}
	fmt.Fprintln(out)
	terminal := false
	if reader, ok := out.(io.Reader); ok {
		terminal = terminalProbe(reader)
	}
	contentType := headers.Get("Content-Type")
	return writeResponseBody(out, []byte(ex.ResponseBody), contentType, terminal)
}

// matchExecution finds the run whose ID equals ref, or the only run whose ID
// starts with ref.
func matchExecution(runs []*domain.Execution, ref string) (*domain.Execution, error) {
	if ref == "" {
		return nil, fmt.Errorf("history: execution ID cannot be empty")
	}
	var matches []*domain.Execution
	for _, ex := range runs {
		if ex.ID == ref {
			return ex, nil
		}
		if strings.HasPrefix(ex.ID, ref) {
			matches = append(matches, ex)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("history: execution %q not found", ref)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("history: execution prefix %q is ambiguous (%d matches)",
			ref, len(matches))
	}
}

// storedHeaders decodes the JSON header map kept with each run. Malformed or
// empty JSON yields no headers rather than an error.
func storedHeaders(raw string) http.Header {
	headers := http.Header{}
	if raw == "" {
		return headers
	}
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return http.Header{}
	}
	return headers
}

func statusLabel(code int) string {
	if code == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", code)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func completeHistoryArgs(st HistoryStore) func(
	*cobra.Command, []string, string,
) ([]string, cobra.ShellCompDirective) {
	completeRequests := CompleteRequestPaths(st.ListCollections, st.ListRequests)
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return completeRequests(cmd, args, toComplete)
		case 1:
			req, err := resolveHistoryRequest(cmd.Context(), st, args[0])
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			runs, err := st.ListExecutionsByRequest(cmd.Context(), req.ID, 0)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			out := make([]string, 0, len(runs))
			for _, ex := range runs {
				if !strings.HasPrefix(ex.ID, toComplete) {
					continue
				}
				out = append(out, fmt.Sprintf("%s\t%s %s", ex.ID,
					ex.CompletedAt.Local().Format(time.DateTime), statusLabel(ex.StatusCode)))
			}
			return out, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
}
