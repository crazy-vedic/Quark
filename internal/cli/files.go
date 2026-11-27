package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/fileformat"
)

// NewExportCmd returns the 'quark export' command, which writes a
// collection as a tree of YAML files.
func NewExportCmd(st fileformat.Source) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <collection> <dir>",
		Short: "Export a collection as git-friendly YAML files",
		Long: "Export a collection, its nested collections and its environments as a\n" +
			"directory of YAML files inside <dir>: one file per request, a collection.yaml\n" +
			"per collection and one file per environment. Running it again replaces the\n" +
			"previous export of the same collection. Import it with 'quark import dir'.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			col, err := resolveCollectionReference(cmd.Context(), st, args[0])
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}
			tree, err := fileformat.Load(cmd.Context(), st, col.ID)
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}
			path, err := fileformat.Write(args[1], tree)
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Exported %d requests from %q to %s\n",
				tree.CountRequests(), tree.Name, path)
			return nil
		},
	}
	cmd.ValidArgsFunction = func(
		c *cobra.Command, args []string, toComplete string,
	) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return CompleteCollectionIDs(st.ListCollections)(c, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	return cmd
}

// NewImportDirCmd returns the 'quark import dir' command, which reads
// collections written by 'quark export'.
func NewImportDirCmd(st fileformat.ApplyStore) *cobra.Command {
	var collectionName, onDuplicate string
	cmd := &cobra.Command{
		Use:   "dir <dir>",
		Short: "Import collections exported with 'quark export'",
		Long: "Import a collection directory written by 'quark export', or every such\n" +
			"directory directly inside <dir>. --on-duplicate works as for import-postman.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			action, err := fileformat.ParseDuplicateAction(onDuplicate)
			if err != nil {
				return err
			}
			dirs, err := fileformat.FindCollections(args[0])
			if err != nil {
				return fmt.Errorf("import dir: %w", err)
			}
			if collectionName != "" && len(dirs) > 1 {
				return fmt.Errorf(
					"import dir: --collection-name needs a single collection; %s holds %d",
					args[0], len(dirs),
				)
			}
			for _, dir := range dirs {
				tree, err := fileformat.Read(dir)
				if err != nil {
					return fmt.Errorf("import dir: %w", err)
				}
				if collectionName != "" {
					tree.Name = collectionName
				}
				res, err := fileformat.Apply(cmd.Context(), st, tree, action)
				if err != nil {
					return fmt.Errorf("import dir: %s: %w", dir, err)
				}
				printDirImportResult(cmd, res)
			}
			return nil
		},
	}
	cmd.Flags().
		StringVarP(&collectionName, "collection-name", "n", "", "Override the imported collection name")
	cmd.Flags().
		StringVar(&onDuplicate, "on-duplicate", string(fileformat.ActionDuplicate),
			"Action when a collection of that name exists: replace, duplicate, merge, skip")
	cmd.ValidArgsFunction = func(
		*cobra.Command, []string, string,
	) ([]cobra.Completion, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	return cmd
}

func printDirImportResult(cmd *cobra.Command, res fileformat.Result) {
	out := cmd.OutOrStdout()
	if res.Action == fileformat.ActionSkip {
		fmt.Fprintf(out, "Skipped %q (%d requests)\n", res.CollectionName, res.Skipped)
		return
	}
	label := ""
	if res.Action != "" {
		label = map[fileformat.DuplicateAction]string{
			fileformat.ActionReplace:   " (replaced)",
			fileformat.ActionDuplicate: " (duplicated)",
			fileformat.ActionMerge:     " (merged)",
		}[res.Action]
	}
	fmt.Fprintf(out, "Imported %d requests and %d environments into %q%s",
		res.Imported, res.Environments, res.CollectionName, label)
	if res.Skipped > 0 {
		fmt.Fprintf(out, ", %d already present", res.Skipped)
	}
	fmt.Fprintln(out)
	for _, w := range res.Warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", w)
	}
}
