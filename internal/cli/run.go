package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/store"
)

// RunStore is the minimum interface NewRunCmd requires.
type RunStore interface {
	store.CollectionLister
	store.RequestReader
	store.ActiveEnvironmentStore
	exec.EnvResolver
}

type requestPathResolver interface {
	ResolveRequestPath(context.Context, string) (*domain.Request, error)
}

// NewRunCmd returns the 'quark run' subcommand.
// "Collection/Request Name" is the first positional argument.
func NewRunCmd(st RunStore, e *exec.Executor) *cobra.Command {
	var namedVars []string
	var verbose bool
	var fail bool

	cmd := &cobra.Command{
		Use:   "run <Collection/Request> [positional-args...]",
		Short: "Execute a saved request",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if resolver, ok := st.(requestPathResolver); ok {
				found, err := resolver.ResolveRequestPath(ctx, args[0])
				if err != nil {
					return fmt.Errorf("run: resolve request: %w", err)
				}
				{
					positionals, overrides, err := parseRunOverrides(args[1:], namedVars)
					if err != nil {
						return fmt.Errorf("run: %w", err)
					}
					found, err = exec.ApplyCollectionDefaults(ctx, st, found)
					if err != nil {
						return fmt.Errorf("run: collection defaults: %w", err)
					}
					colEnv, globalEnv, err := exec.ResolveEnvVars(
						ctx,
						st,
						found.CollectionID,
					)
					if err != nil {
						return fmt.Errorf("run: resolve environments: %w", err)
					}
					prepared, err := exec.InterpolateRequestWithOverrides(
						found,
						positionals,
						overrides,
						colEnv,
						globalEnv,
					)
					if err != nil {
						return fmt.Errorf("run: interpolate: %w", err)
					}
					result, err := e.Execute(ctx, prepared)
					if err != nil {
						return fmt.Errorf("run: execute: %w", err)
					}
					return finishRun(cmd, prepared, result, verbose, fail)
				}
			}
			parts := strings.SplitN(args[0], "/", 2)
			if len(parts) != 2 {
				return fmt.Errorf(
					"run: argument must be 'Collection/Request Name', got %q",
					args[0],
				)
			}
			collectionName, requestName := parts[0], parts[1]

			cols, err := st.ListCollections(ctx)
			if err != nil {
				return fmt.Errorf("run: list collections: %w", err)
			}
			var collectionID string
			for _, c := range cols {
				if c.Name == collectionName {
					collectionID = c.ID
					break
				}
			}
			if collectionID == "" {
				return fmt.Errorf("run: collection %q not found", collectionName)
			}

			reqs, err := st.ListRequests(ctx, collectionID)
			if err != nil {
				return fmt.Errorf("run: list requests: %w", err)
			}
			var found *domain.Request
			for _, r := range reqs {
				if r.Name == requestName {
					found = r
					break
				}
			}
			if found == nil {
				return fmt.Errorf("run: request %q not found in collection %q",
					requestName, collectionName)
			}

			positionals, overrides, err := parseRunOverrides(args[1:], namedVars)
			if err != nil {
				return fmt.Errorf("run: %w", err)
			}

			found, err = exec.ApplyCollectionDefaults(ctx, st, found)
			if err != nil {
				return fmt.Errorf("run: collection defaults: %w", err)
			}
			colEnv, globalEnv, err := exec.ResolveEnvVars(
				ctx, st, collectionID)
			if err != nil {
				return fmt.Errorf("run: resolve environments: %w", err)
			}
			prepared, err := exec.InterpolateRequestWithOverrides(
				found,
				positionals,
				overrides,
				colEnv,
				globalEnv,
			)
			if err != nil {
				return fmt.Errorf("run: interpolate: %w", err)
			}

			result, err := e.Execute(ctx, prepared)
			if err != nil {
				return fmt.Errorf("run: execute: %w", err)
			}

			return finishRun(cmd, prepared, result, verbose, fail)
		},
	}
	cmd.ValidArgsFunction = CompleteRequestPaths(st.ListCollections, st.ListRequests)
	cmd.Flags().BoolVar(&fail, "fail", false,
		"Exit with an error when the response status is 400 or higher")
	cmd.Flags().StringArrayVarP(
		&namedVars,
		"var",
		"v",
		nil,
		"Override a request variable as key=value; overrides collection/global envs",
	)
	cmd.Flags().BoolVar(
		&verbose,
		"verbose",
		false,
		"Print request and response headers and request details (may include credentials)",
	)
	return cmd
}

func parseRunOverrides(
	positionals []string,
	assignments []string,
) ([]string, map[string]string, error) {
	named := make(map[string]string, len(assignments))
	for _, assignment := range assignments {
		key, value, ok := strings.Cut(assignment, "=")
		if !ok {
			return nil, nil, fmt.Errorf("invalid --var %q: expected key=value", assignment)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, nil, fmt.Errorf("invalid --var %q: key cannot be empty", assignment)
		}
		named[key] = value
	}
	return positionals, named, nil
}

// finishRun prints the result and, with --fail, turns an HTTP error status into
// a command error so scripts and CI can detect it.
func finishRun(
	cmd *cobra.Command,
	req *domain.Request,
	result *exec.ExecuteResult,
	verbose, fail bool,
) error {
	if err := printRunResult(cmd, req, result, verbose); err != nil {
		return err
	}
	if fail && result.StatusCode >= 400 {
		return fmt.Errorf("run: HTTP %s", result.Status)
	}
	return nil
}
