package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/schedule"
	"github.com/crazy-vedic/quark/internal/store"
)

const scheduleRunDueCmd = "run-due"

// staleRunAge is how long a run may stay "running" before it is treated as
// interrupted. It is longer than any normal request timeout.
const staleRunAge = 15 * time.Minute

type staleRunRecoverer interface {
	RecoverStaleScheduledRuns(ctx context.Context, cutoff time.Time) (int, error)
}

type ScheduleStore interface {
	SearchStore
	ScheduleEnvironmentStore
	ScheduleRunStore
}

type ScheduleEnvironmentStore interface {
	store.ActiveEnvironmentStore
	exec.EnvResolver
}

type ScheduleRunStore interface {
	store.ScheduledRunReader
	store.ScheduledRunWriter
}

func NewScheduleCmd(st ScheduleStore, e *exec.Executor, now func() time.Time) *cobra.Command {
	if now == nil {
		now = time.Now
	}
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Schedule delayed request execution",
	}
	cmd.AddCommand(newScheduleAddCmd(st, now))
	cmd.AddCommand(newScheduleListCmd(st))
	cmd.AddCommand(newScheduleCancelCmd(st))
	cmd.AddCommand(newScheduleRunDueCmd(st, e, now))
	return cmd
}

func newScheduleAddCmd(st ScheduleStore, now func() time.Time) *cobra.Command {
	var when string
	cmd := &cobra.Command{
		Use:   "add <Collection/Request>",
		Short: "Schedule a saved request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(when) == "" {
				return fmt.Errorf("schedule add: --at is required")
			}
			req, err := resolveScheduleRequest(cmd.Context(), st, args[0])
			if err != nil {
				return fmt.Errorf("schedule add: %w", err)
			}
			runAt, err := schedule.ParseWhen(when, now())
			if err != nil {
				return fmt.Errorf("schedule add: %w", err)
			}
			run := &domain.ScheduledRun{
				ID:        uuid.New().String(),
				RequestID: req.ID,
				RunAt:     runAt,
				Status:    domain.ScheduledRunPending,
			}
			if err := st.SaveScheduledRun(cmd.Context(), run); err != nil {
				return fmt.Errorf("schedule add: save: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Scheduled %s at %s (%s)\n",
				args[0], runAt.Format(time.RFC3339), shortScheduleID(run.ID))
			return nil
		},
	}
	cmd.ValidArgsFunction = CompleteRequestPaths(st.ListCollections, st.ListRequests)
	cmd.Flags().
		StringVar(&when, "at", "", "Run time: duration, 'in 10m', RFC3339, or 'YYYY-MM-DD HH:MM'")
	return cmd
}

func newScheduleListCmd(st ScheduleStore) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List scheduled runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			runs, err := st.ListScheduledRuns(ctx)
			if err != nil {
				return fmt.Errorf("schedule list: %w", err)
			}
			if len(runs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No scheduled runs.")
				return nil
			}
			paths, err := scheduleCollectionPaths(ctx, st)
			if err != nil {
				return fmt.Errorf("schedule list: %w", err)
			}
			for _, run := range runs {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %-9s  %s  %s\n",
					shortScheduleID(run.ID),
					run.Status,
					run.RunAt.Format(time.RFC3339),
					scheduledRequestPath(ctx, st, paths, run.RequestID),
				)
			}
			return nil
		},
	}
}

func newScheduleCancelCmd(st ScheduleStore) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a pending scheduled run",
		Long: "Cancel a pending scheduled run so it never fires. " +
			"Accepts the full run ID or the unique prefix shown by `schedule list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			run, err := resolveScheduledRun(ctx, st, args[0])
			if err != nil {
				return fmt.Errorf("schedule cancel: %w", err)
			}
			id := shortScheduleID(run.ID)
			switch run.Status {
			case domain.ScheduledRunCancelled:
				fmt.Fprintf(cmd.OutOrStdout(), "Already cancelled %s\n", id)
				return nil
			case domain.ScheduledRunPending:
			default:
				return fmt.Errorf(
					"schedule cancel: run %s is %s; only pending runs can be cancelled",
					id, run.Status)
			}
			// Claiming first makes the cancel atomic with respect to run-due and
			// the TUI: whoever claims a pending run owns it, so a run cannot be
			// cancelled after it has started.
			claimed, err := st.ClaimScheduledRun(ctx, run.ID)
			if err != nil {
				return fmt.Errorf("schedule cancel: %w", err)
			}
			if !claimed {
				return fmt.Errorf("schedule cancel: run %s is no longer pending", id)
			}
			run.Status = domain.ScheduledRunCancelled
			run.LastError = ""
			if err := st.SaveScheduledRun(ctx, run); err != nil {
				return fmt.Errorf("schedule cancel: save: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cancelled %s\n", id)
			return nil
		},
	}
}

// resolveScheduledRun matches a run by full ID or by an ID prefix of at least
// minIDPrefixLength characters, the length schedule list prints.
func resolveScheduledRun(
	ctx context.Context,
	st ScheduleStore,
	reference string,
) (*domain.ScheduledRun, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("run ID is required")
	}
	runs, err := st.ListScheduledRuns(ctx)
	if err != nil {
		return nil, err
	}
	var matches []*domain.ScheduledRun
	for _, run := range runs {
		if run.ID == reference ||
			(len(reference) >= minIDPrefixLength && strings.HasPrefix(run.ID, reference)) {
			matches = append(matches, run)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("scheduled run %q not found", reference)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("scheduled run %q is ambiguous; use more of its ID", reference)
	}
}

// scheduleCollectionPaths maps each collection ID to its full Collection path.
// Stores that support nested collections report the full path; flat stores
// fall back to the collection name.
func scheduleCollectionPaths(ctx context.Context, st ScheduleStore) (map[string]string, error) {
	cols, err := st.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	nested, hasNested := st.(collectionFullPathResolver)
	paths := make(map[string]string, len(cols))
	for _, col := range cols {
		path := col.Name
		if hasNested {
			if full, err := nested.CollectionPath(ctx, col.ID); err == nil {
				path = full
			}
		}
		paths[col.ID] = path
	}
	return paths, nil
}

// scheduledRequestPath returns the Collection/Request path of a scheduled
// request. A request that no longer exists is shown by its short ID.
func scheduledRequestPath(
	ctx context.Context,
	st ScheduleStore,
	collectionPaths map[string]string,
	requestID string,
) string {
	req, err := st.GetRequest(ctx, requestID)
	if err != nil || req == nil {
		return "(missing request " + shortID(requestID) + ")"
	}
	colPath, ok := collectionPaths[req.CollectionID]
	if !ok {
		return "(unknown collection)/" + req.Name
	}
	return colPath + "/" + req.Name
}

func newScheduleRunDueCmd(st ScheduleStore, e *exec.Executor, now func() time.Time) *cobra.Command {
	return &cobra.Command{
		Use:   scheduleRunDueCmd,
		Short: "Execute all pending scheduled runs due now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A run still "running" long after its request could have finished
			// belongs to a process that died. Mark it failed so it is visible.
			if recoverer, ok := st.(staleRunRecoverer); ok {
				n, err := recoverer.RecoverStaleScheduledRuns(
					cmd.Context(), now().Add(-staleRunAge))
				if err != nil {
					return fmt.Errorf("schedule run-due: recover: %w", err)
				}
				if n > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "recovered %d interrupted run(s)\n", n)
				}
			}
			runs, err := st.ListDueScheduledRuns(cmd.Context(), now())
			if err != nil {
				return fmt.Errorf("schedule run-due: %w", err)
			}
			if len(runs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No due scheduled runs.")
				return nil
			}
			failed := 0
			for _, run := range runs {
				claimed, err := st.ClaimScheduledRun(cmd.Context(), run.ID)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "failed %s: %v\n", shortScheduleID(run.ID), err)
					continue
				}
				if !claimed {
					// Another quark process (cron run-due or an open TUI) took it.
					continue
				}
				run.Status = domain.ScheduledRunRunning
				run.LastError = ""

				req, err := st.GetRequest(cmd.Context(), run.RequestID)
				if err != nil {
					_ = markScheduledRunFailed(cmd.Context(), st, run, err)
					fmt.Fprintf(cmd.OutOrStdout(), "failed %s: %v\n", shortScheduleID(run.ID), err)
					failed++
					continue
				}
				var prepared *domain.Request
				var colEnv, globalEnv map[string]string
				colEnv, globalEnv, err = exec.ResolveEnvVars(
					cmd.Context(), st, req.CollectionID,
				)
				if err != nil {
					err = fmt.Errorf("resolve environments: %w", err)
				}
				if err == nil {
					prepared, err = exec.InterpolateRequestWithOverrides(
						req, nil, nil, colEnv, globalEnv,
					)
				}
				var result *exec.ExecuteResult
				if err == nil {
					result, err = e.Execute(cmd.Context(), prepared)
				}
				if result != nil {
					// run-due never shows the body. A large response is streamed
					// to a temp file that only Cleanup removes.
					_ = result.Cleanup()
				}
				if err != nil || (result != nil && result.StatusCode >= 500) {
					if err == nil {
						err = fmt.Errorf("HTTP %d", result.StatusCode)
					}
					_ = markScheduledRunFailed(cmd.Context(), st, run, err)
					fmt.Fprintf(cmd.OutOrStdout(), "failed %s: %v\n", shortScheduleID(run.ID), err)
					failed++
					continue
				}
				run.Status = domain.ScheduledRunCompleted
				run.LastError = ""
				if err := st.SaveScheduledRun(cmd.Context(), run); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "failed to mark %s completed: %v\n",
						shortScheduleID(run.ID), err)
					failed++
					continue
				}
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"ran %s request=%s\n",
					shortScheduleID(run.ID),
					run.RequestID,
				)
			}
			if failed > 0 {
				// Non-zero exit so cron jobs and CI can see the failure.
				return fmt.Errorf("schedule run-due: %d run(s) failed", failed)
			}
			return nil
		},
	}
}

func shortScheduleID(id string) string { return shortID(id) }

func markScheduledRunFailed(
	ctx context.Context,
	scheduler store.ScheduledRunWriter,
	run *domain.ScheduledRun,
	runErr error,
) error {
	run.Status = domain.ScheduledRunFailed
	run.LastError = runErr.Error()
	return scheduler.SaveScheduledRun(ctx, run)
}

// resolveScheduleRequest resolves a request path the way `quark run` does:
// full nested paths with ambiguity errors when the store supports it.
func resolveScheduleRequest(
	ctx context.Context,
	st ScheduleStore,
	path string,
) (*domain.Request, error) {
	if resolver, ok := st.(requestPathResolver); ok {
		return resolver.ResolveRequestPath(ctx, path)
	}
	req, _, err := findRequestByPath(ctx, st, path)
	return req, err
}

func findRequestByPath(
	ctx context.Context,
	st interface {
		store.CollectionLister
		store.RequestReader
	},
	path string,
) (*domain.Request, string, error) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("argument must be 'Collection/Request Name', got %q", path)
	}
	collectionName, requestName := parts[0], parts[1]
	cols, err := st.ListCollections(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list collections: %w", err)
	}
	for _, col := range cols {
		if col.Name != collectionName {
			continue
		}
		reqs, err := st.ListRequests(ctx, col.ID)
		if err != nil {
			return nil, "", fmt.Errorf("list requests: %w", err)
		}
		for _, req := range reqs {
			if req.Name == requestName {
				return req, col.Name, nil
			}
		}
		return nil, "", fmt.Errorf(
			"request %q not found in collection %q",
			requestName,
			collectionName,
		)
	}
	return nil, "", fmt.Errorf("collection %q not found", collectionName)
}
