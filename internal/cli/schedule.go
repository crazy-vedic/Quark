package cli

import (
	"context"
	"errors"
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

type ScheduleStore interface {
	SearchStore
	ScheduleEnvironmentStore
	ScheduleRunStore
}

type ScheduleEnvironmentStore interface {
	store.EnvironmentReader
	store.ActiveEnvironmentStore
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
			req, _, err := findRequestByPath(cmd.Context(), st, args[0])
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
			runs, err := st.ListScheduledRuns(cmd.Context())
			if err != nil {
				return fmt.Errorf("schedule list: %w", err)
			}
			if len(runs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No scheduled runs.")
				return nil
			}
			for _, run := range runs {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %-9s  %s  request=%s\n",
					shortScheduleID(run.ID),
					run.Status,
					run.RunAt.Format(time.RFC3339),
					run.RequestID,
				)
			}
			return nil
		},
	}
}

func newScheduleRunDueCmd(st ScheduleStore, e *exec.Executor, now func() time.Time) *cobra.Command {
	return &cobra.Command{
		Use:   "run-due",
		Short: "Execute all pending scheduled runs due now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runs, err := st.ListDueScheduledRuns(cmd.Context(), now())
			if err != nil {
				return fmt.Errorf("schedule run-due: %w", err)
			}
			if len(runs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No due scheduled runs.")
				return nil
			}
			for _, run := range runs {
				req, err := st.GetRequest(cmd.Context(), run.RequestID)
				if err != nil {
					if saveErr := markScheduledRunFailed(
						cmd.Context(),
						st,
						run,
						err,
					); saveErr != nil {
						return fmt.Errorf(
							"schedule run-due: mark %s failed after request load error: %w",
							run.ID,
							errors.Join(err, saveErr),
						)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "failed %s: %v\n", shortScheduleID(run.ID), err)
					continue
				}
				activeEnvID, err := st.GetActiveEnvironment(cmd.Context(), req.CollectionID)
				if err != nil {
					activeEnvID = ""
				}
				colEnv, globalEnv := exec.ResolveEnvVars(
					cmd.Context(),
					st,
					activeEnvID,
					req.CollectionID,
				)
				prepared, err := exec.InterpolateRequestWithOverrides(
					req,
					nil,
					nil,
					colEnv,
					globalEnv,
				)
				if err == nil {
					_, err = e.Execute(cmd.Context(), prepared)
				}
				if err != nil {
					if saveErr := markScheduledRunFailed(
						cmd.Context(),
						st,
						run,
						err,
					); saveErr != nil {
						return fmt.Errorf(
							"schedule run-due: mark %s failed after execution error: %w",
							run.ID,
							errors.Join(err, saveErr),
						)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "failed %s: %v\n", shortScheduleID(run.ID), err)
					continue
				}
				run.Status = domain.ScheduledRunCompleted
				run.LastError = ""
				if err := st.SaveScheduledRun(cmd.Context(), run); err != nil {
					return fmt.Errorf("schedule run-due: update %s: %w", run.ID, err)
				}
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"ran %s request=%s\n",
					shortScheduleID(run.ID),
					run.RequestID,
				)
			}
			return nil
		},
	}
}

func shortScheduleID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

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
