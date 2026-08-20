package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

const cliDefaultEnvironmentName = "default"
const cliGlobalEnvironmentName = "global"

// EnvStore is the minimum interface required by the env CLI.
type EnvStore interface {
	store.CollectionLister
	store.EnvironmentReader
	store.EnvironmentWriter
	store.ActiveEnvironmentStore
	GetEnvironmentByName(
		ctx context.Context,
		collectionID, name string,
	) (*domain.Environment, error)
}

// NewEnvCmd returns the 'quark env' subcommand.
func NewEnvCmd(st EnvStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environments",
	}

	listCmd := &cobra.Command{
		Use:   "list [<collection>]",
		Short: "List environments (all, or for a specific collection)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var collectionID string
			if len(args) > 0 {
				collectionID = args[0]
			}
			if collectionID != "" {
				col, err := resolveCollectionReference(cmd.Context(), st, collectionID)
				if err != nil {
					return fmt.Errorf("env list: resolve collection: %w", err)
				}
				collectionID = col.ID
			}
			return envList(cmd.Context(), st, collectionID, cmd.OutOrStdout())
		},
	}
	if st != nil {
		listCmd.ValidArgsFunction = CompleteCollectionIDs(st.ListCollections)
	}
	cmd.AddCommand(listCmd)

	createCmd := &cobra.Command{
		Use:   "create <collection> <name>",
		Short: "Create a new environment for a collection",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			col, err := resolveCollectionReference(cmd.Context(), st, args[0])
			if err != nil {
				return fmt.Errorf("env create: resolve collection: %w", err)
			}
			return envCreate(cmd.Context(), st, col.ID, args[1])
		},
	}
	if st != nil {
		createCmd.ValidArgsFunction = CompleteCollectionIDs(st.ListCollections)
	}
	cmd.AddCommand(createCmd)

	setCmd := &cobra.Command{
		Use:   "set <collection> <env-name> <key> <value>",
		Short: "Set a key-value pair in an environment",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			col, err := resolveCollectionReference(cmd.Context(), st, args[0])
			if err != nil {
				return fmt.Errorf("env set: resolve collection: %w", err)
			}
			return envSet(cmd.Context(), st, col.ID, args[1], args[2], args[3])
		},
	}
	if st != nil {
		setCmd.ValidArgsFunction = CompleteCollectionThenEnvironment(
			st.ListCollections,
			st.ListCollectionEnvironments,
		)
	}
	cmd.AddCommand(setCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "set-global <key> <value>",
		Short: "Set a key-value pair in the global environment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return envSetGlobal(cmd.Context(), st, args[0], args[1])
		},
	})

	deleteCmd := &cobra.Command{
		Use:   "delete <collection> <env-name>",
		Short: "Delete an environment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			col, err := resolveCollectionReference(cmd.Context(), st, args[0])
			if err != nil {
				return fmt.Errorf("env delete: resolve collection: %w", err)
			}
			return envDelete(cmd.Context(), st, col.ID, args[1])
		},
	}
	if st != nil {
		deleteCmd.ValidArgsFunction = CompleteCollectionThenEnvironment(
			st.ListCollections,
			st.ListCollectionEnvironments,
		)
	}
	cmd.AddCommand(deleteCmd)

	activeCmd := &cobra.Command{
		Use:   "active <collection> <env-name>",
		Short: "Set the active environment for a collection (used by TUI and quark run)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			col, err := resolveCollectionReference(cmd.Context(), st, args[0])
			if err != nil {
				return fmt.Errorf("env active: resolve collection: %w", err)
			}
			return envActive(cmd.Context(), st, col.ID, args[1], cmd.OutOrStdout())
		},
	}
	if st != nil {
		activeCmd.ValidArgsFunction = CompleteCollectionThenEnvironment(
			st.ListCollections,
			st.ListCollectionEnvironments,
		)
	}
	cmd.AddCommand(activeCmd)

	return cmd
}

func envList(ctx context.Context, st EnvStore, collectionID string, w io.Writer) error {
	// List global envs.
	globals, err := st.ListEnvironments(ctx, "")
	if err != nil {
		return fmt.Errorf("list global envs: %w", err)
	}
	fmt.Fprintln(w, "Global environments:")
	for _, e := range globals {
		vars := e.Vars()
		fmt.Fprintf(w, "  %s (%d vars)\n", e.Name, len(vars))
	}

	if collectionID != "" {
		// List collection envs.
		envs, err := st.ListCollectionEnvironments(ctx, collectionID)
		if err != nil {
			return fmt.Errorf("list envs: %w", err)
		}
		fmt.Fprintf(w, "\nCollection environments (%s):\n", collectionID)
		for _, e := range envs {
			vars := e.Vars()
			fmt.Fprintf(w, "  %s (%d vars)\n", e.Name, len(vars))
		}
		return nil
	}

	// No collection ID: list all environments in one query, then group by collection.
	envs, err := st.ListAllEnvironments(ctx)
	if err != nil {
		return fmt.Errorf("list all envs: %w", err)
	}
	cols, err := st.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("list collections: %w", err)
	}
	if len(cols) == 0 {
		fmt.Fprintln(w, "\nNo collections found.")
		return nil
	}
	// Build map: collection ID -> name for lookup.
	colNames := make(map[string]string, len(cols))
	for _, col := range cols {
		colNames[col.ID] = col.Name
	}
	// Group envs by collection ID.
	grouped := make(map[string][]*domain.Environment)
	for _, e := range envs {
		grouped[e.CollectionID] = append(grouped[e.CollectionID], e)
	}
	for _, col := range cols {
		fmt.Fprintf(w, "\nCollection environments (%s):\n", col.Name)
		for _, e := range grouped[col.ID] {
			vars := e.Vars()
			fmt.Fprintf(w, "  %s (%d vars)\n", e.Name, len(vars))
		}
	}
	return nil
}

func envCreate(ctx context.Context, st EnvStore, collectionID, name string) error {
	env := &domain.Environment{
		ID:           uuid.New().String(),
		CollectionID: collectionID,
		Name:         name,
		Data:         "{}",
		SortOrder:    0,
	}
	if err := st.SaveEnvironment(ctx, env); err != nil {
		return fmt.Errorf("create env: %w", err)
	}
	fmt.Printf("Environment %q created for collection %s\n", name, collectionID)
	return nil
}

func envSet(ctx context.Context, st EnvStore, collectionID, envName, key, value string) error {
	env, err := st.GetEnvironmentByName(ctx, collectionID, envName)
	if err != nil {
		return fmt.Errorf("get env: %w", err)
	}
	vars := env.Vars()
	if vars == nil {
		vars = make(map[string]string)
	}
	vars[key] = value
	env.SetVars(vars)
	if err := st.SaveEnvironment(ctx, env); err != nil {
		return fmt.Errorf("save env: %w", err)
	}
	fmt.Printf("Set %s = %s in %s/%s\n", key, value, collectionID, envName)
	return nil
}

func envSetGlobal(ctx context.Context, st EnvStore, key, value string) error {
	env, err := st.GetGlobalEnvironment(ctx)
	if err != nil {
		return fmt.Errorf("get global env: %w", err)
	}
	vars := env.Vars()
	if vars == nil {
		vars = make(map[string]string)
	}
	vars[key] = value
	env.SetVars(vars)
	if err := st.SaveEnvironment(ctx, env); err != nil {
		return fmt.Errorf("save global env: %w", err)
	}
	fmt.Printf("Set %s = %s in global env\n", key, value)
	return nil
}

func envDelete(ctx context.Context, st EnvStore, collectionID, envName string) error {
	env, err := st.GetEnvironmentByName(ctx, collectionID, envName)
	if err != nil {
		return fmt.Errorf("get env: %w", err)
	}
	if env.Name == cliDefaultEnvironmentName {
		return fmt.Errorf("cannot delete the default environment")
	}
	if err := st.DeleteEnvironment(ctx, env.ID); err != nil {
		return fmt.Errorf("delete env: %w", err)
	}
	fmt.Printf("Deleted environment %q from collection %s\n", envName, collectionID)
	return nil
}

func envActive(
	ctx context.Context,
	st EnvStore,
	collectionID, envName string,
	w io.Writer,
) error {
	if envName == cliGlobalEnvironmentName {
		return fmt.Errorf("cannot set global as a collection active environment")
	}
	env, err := st.GetEnvironmentByName(ctx, collectionID, envName)
	if err != nil {
		return fmt.Errorf("get env: %w", err)
	}
	if err := st.SetActiveEnvironment(ctx, collectionID, env.ID); err != nil {
		return fmt.Errorf("set active env: %w", err)
	}
	fmt.Fprintf(w, "Active environment set for %s: %s\n", collectionID, envName)
	return nil
}
