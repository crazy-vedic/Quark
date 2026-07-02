package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
)

type ListCollectionsFunc func(context.Context) ([]*domain.Collection, error)
type ListRequestsFunc func(context.Context, string) ([]*domain.Request, error)
type ListCollectionEnvironmentsFunc func(context.Context, string) ([]*domain.Environment, error)

// CompleteCollectionIDs returns collection ID suggestions with collection names
// as descriptions so shells can show human-friendly labels.
func CompleteCollectionIDs(listCollections ListCollectionsFunc) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cols, err := listCollections(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(cols))
		for _, col := range cols {
			if !matchesCompletion(col.ID, toComplete) && !matchesCompletion(col.Name, toComplete) {
				continue
			}
			out = append(out, col.ID+"\t"+col.Name)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteRequestPaths suggests Collection/Request paths for commands like
// `quark run` and `quark schedule add`.
func CompleteRequestPaths(
	listCollections ListCollectionsFunc,
	listRequests ListRequestsFunc,
) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cols, err := listCollections(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		collectionPrefix, requestPrefix, hasSlash := strings.Cut(toComplete, "/")
		if !hasSlash {
			out := make([]string, 0, len(cols))
			for _, col := range cols {
				if !matchesCompletion(col.Name, toComplete) {
					continue
				}
				out = append(out, col.Name+"/\tcollection")
			}
			return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
		}

		var out []string
		for _, col := range cols {
			if !matchesCompletion(col.Name, collectionPrefix) {
				continue
			}
			reqs, err := listRequests(cmd.Context(), col.ID)
			if err != nil {
				continue
			}
			for _, req := range reqs {
				if !matchesCompletion(req.Name, requestPrefix) {
					continue
				}
				out = append(out, col.Name+"/"+req.Name+"\t"+req.Method)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// CompleteCollectionThenEnvironment first completes collection IDs, then
// environment names for the chosen collection.
func CompleteCollectionThenEnvironment(
	listCollections ListCollectionsFunc,
	listEnvironments ListCollectionEnvironmentsFunc,
) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		switch len(args) {
		case 0:
			return CompleteCollectionIDs(listCollections)(cmd, args, toComplete)
		case 1:
			envs, err := listEnvironments(cmd.Context(), args[0])
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			out := make([]string, 0, len(envs))
			for _, env := range envs {
				if !matchesCompletion(env.Name, toComplete) {
					continue
				}
				out = append(out, env.Name)
			}
			return out, cobra.ShellCompDirectiveNoFileComp
		default:
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}
}

func matchesCompletion(value, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix))
}
