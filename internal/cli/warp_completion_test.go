package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarpCompletionPluginGeneratedFromCobraTree(t *testing.T) {
	root := newWarpCompletionTestRoot()
	plugin, err := WarpCompletionPlugin(root)
	require.NoError(t, err)

	assert.Contains(t, plugin, "export function activate(warp)")
	assert.Contains(t, plugin, "warp.completions.registerCommandSignature")
	assert.Contains(t, plugin, `"name": "quark"`)
	assert.Contains(t, plugin, `"name": "collection"`)
	assert.Contains(t, plugin, `"name": "create"`)
	assert.Contains(t, plugin, `"name": "run-due"`)
	assert.Contains(t, plugin, `"name": "synthetic"`)
	assert.Contains(t, plugin, `"name": "child"`)
}

func TestWarpCompletionPluginGeneratedFromCobraFlags(t *testing.T) {
	root := newWarpCompletionTestRoot()
	plugin, err := WarpCompletionPlugin(root)
	require.NoError(t, err)

	assert.Contains(t, plugin, `"name": "--config"`)
	assert.Contains(t, plugin, `"name": "--dim"`)
	assert.Contains(t, plugin, `"name": "--collection"`)
	assert.Contains(t, plugin, `"name": "--at"`)
	assert.Contains(t, plugin, `"-h"`)
	assert.Contains(t, plugin, `"--help"`)
}

func TestWarpCompletionPluginHydratesCobraCompletionHooks(t *testing.T) {
	root := newWarpCompletionTestRoot()
	plugin, err := WarpCompletionPlugin(root)
	require.NoError(t, err)

	assert.Contains(t, plugin, "function parseCobraCompletion(output)")
	assert.Contains(t, plugin, "function prefixIndex(tokens, prefix)")
	assert.Contains(t, plugin, "function cobraGenerator(prefix, completedBeforeCurrent = 0)")
	assert.Contains(t, plugin, "hydrateDynamicValues(quarkSignature)")
	assert.Contains(t, plugin, `quark __complete`)
	assert.Contains(t, plugin, `"prefix": [`)
	assert.Contains(t, plugin, `"run"`)
	assert.Contains(t, plugin, `"request"`)
	assert.Contains(t, plugin, `"list"`)
	assert.Contains(t, plugin, `"--collection"`)
	assert.Contains(t, plugin, `"quarkCobraGenerator"`)
	assert.Contains(t, plugin, `"completedBeforeCurrent": 1`)
}

func TestWarpCompletionPluginSkipsHiddenCommands(t *testing.T) {
	root := newWarpCompletionTestRoot()
	plugin, err := WarpCompletionPlugin(root)
	require.NoError(t, err)

	assert.NotContains(t, plugin, "__warp_completion_plugin")
	assert.NotContains(t, plugin, "hidden-test")
}

func TestWarpCompletionPluginCommandEmitsPluginFromRoot(t *testing.T) {
	root := newWarpCompletionTestRoot()
	root.AddCommand(NewWarpCompletionPluginCmd())
	root.SetArgs([]string{"__warp_completion_plugin"})
	var out bytes.Buffer
	root.SetOut(&out)

	require.NoError(t, root.Execute())
	assert.Contains(t, out.String(), `"name": "synthetic"`)
	assert.NotContains(t, out.String(), "__warp_completion_plugin")
}

func TestWarpCompletionPluginNilRootErrors(t *testing.T) {
	_, err := WarpCompletionPlugin(nil)
	require.Error(t, err)
}

func newWarpCompletionTestRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "quark",
		Short: "A keyboard-driven TUI HTTP client",
	}
	root.PersistentFlags().String("config", "./.quark", "Directory for config and data files")
	root.PersistentFlags().Bool("debug", false, "Log all keystrokes")
	root.PersistentFlags().String("dim", "", "Force TUI density: wide|narrow|tiny|absurd")

	collection := &cobra.Command{Use: "collection", Short: "Manage collections"}
	collection.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a new collection",
	})
	root.AddCommand(collection)

	request := &cobra.Command{Use: "request", Short: "Manage requests"}
	requestList := &cobra.Command{Use: "list", Short: "List requests in a collection"}
	requestList.Flags().String("collection", "", "Collection ID")
	_ = requestList.RegisterFlagCompletionFunc("collection", noFileCompletion)
	request.AddCommand(requestList)
	root.AddCommand(request)

	run := &cobra.Command{
		Use:               "run <Collection/Request>",
		Short:             "Execute a saved request",
		ValidArgsFunction: noFileCompletion,
	}
	root.AddCommand(run)

	schedule := &cobra.Command{Use: "schedule", Short: "Schedule delayed request execution"}
	add := &cobra.Command{
		Use:               "add <Collection/Request>",
		Short:             "Schedule a saved request",
		ValidArgsFunction: noFileCompletion,
	}
	add.Flags().String("at", "", "Run time")
	schedule.AddCommand(add)
	schedule.AddCommand(&cobra.Command{Use: "run-due", Short: "Execute due runs"})
	root.AddCommand(schedule)

	env := &cobra.Command{Use: "env", Short: "Manage environments"}
	envSet := &cobra.Command{
		Use:               "set <collection-id> <env-name> <key> <value>",
		Short:             "Set an environment variable",
		ValidArgsFunction: noFileCompletion,
	}
	env.AddCommand(envSet)
	root.AddCommand(env)

	synthetic := &cobra.Command{Use: "synthetic", Short: "Synthetic command"}
	synthetic.AddCommand(&cobra.Command{Use: "child", Short: "Synthetic child"})
	root.AddCommand(synthetic)

	root.AddCommand(&cobra.Command{Use: "hidden-test", Hidden: true})
	return root
}

func noFileCompletion(
	*cobra.Command,
	[]string,
	string,
) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}
