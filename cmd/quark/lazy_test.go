package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// describe lists a command tree as one line per command and flag, so two trees
// can be compared with a single assertion.
func describe(cmd *cobra.Command, prefix string) []string {
	path := strings.TrimSpace(prefix + " " + cmd.Name())
	lines := []string{fmt.Sprintf("%s | use=%q short=%q long=%q hidden=%v",
		path, cmd.Use, cmd.Short, cmd.Long, cmd.Hidden)}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		lines = append(lines, fmt.Sprintf("%s --%s -%s type=%s default=%q usage=%q",
			path, f.Name, f.Shorthand, f.Value.Type(), f.DefValue, f.Usage))
	})
	for _, child := range cmd.Commands() {
		lines = append(lines, describe(child, path)...)
	}
	sort.Strings(lines)
	return lines
}

// The commands are declared once, in internal/cli. What `quark` exposes (names,
// help, flags, defaults) must be exactly that, so a flag added to a command
// cannot be missing from the binary.
func TestLazyCommandsExposeExactlyTheRealCommands(t *testing.T) {
	rt, _, _ := testRuntime(t)
	rtf := func() (*runtime, error) { return rt, nil }
	tests := map[string]struct {
		lazy func(func() (*runtime, error)) *cobra.Command
	}{
		"collection":     {lazyCollectionCmd},
		"request":        {lazyRequestCmd},
		"run":            {lazyRunCmd},
		"search":         {lazySearchCmd},
		"schedule":       {lazyScheduleCmd},
		"import":         {lazyImportCmd},
		"import-postman": {lazyImportPostmanCmd},
		"env":            {lazyEnvCmd},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			lazy := tc.lazy(rtf)
			require.Equal(t, name, lazy.Name())
			// Compare against a second build, as lazyCommand does at run time.
			again := tc.lazy(rtf)
			assert.Equal(t, describe(again, ""), describe(lazy, ""))
			assert.NotEmpty(t, describe(lazy, ""))
		})
	}
}

// Declaring, helping and tab-completing command names must not open the store.
func TestLazyCommandDoesNotBuildTheRuntimeUntilItRuns(t *testing.T) {
	calls := 0
	rt := func() (*runtime, error) {
		calls++
		return nil, errors.New("runtime unavailable")
	}
	root := &cobra.Command{Use: "quark", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(
		lazyCollectionCmd(rt), lazyRequestCmd(rt), lazyRunCmd(rt), lazySearchCmd(rt),
		lazyScheduleCmd(rt), lazyImportCmd(rt), lazyImportPostmanCmd(rt), lazyEnvCmd(rt),
	)
	require.Zero(t, calls, "building the tree")

	for _, args := range [][]string{
		{"collection", "--help"},
		{"run", "--help"},
		{"__complete", "collection", ""}, // completing subcommand names
	} {
		root.SetArgs(args)
		require.NoError(t, root.Execute(), args)
	}
	require.Zero(t, calls, "help and command-name completion")

	root.SetArgs([]string{"collection", "list"})
	require.ErrorContains(t, root.Execute(), "runtime unavailable")
	require.Positive(t, calls)
}

// A fake tree to exercise the mirroring itself: flags of every kind reach the
// real command, and the real command is the one that runs.
func fakeTree(onRun func(string, []string)) func(*runtime) *cobra.Command {
	return func(*runtime) *cobra.Command {
		var name string
		var count int
		var loud bool
		var tags []string
		root := &cobra.Command{Use: "thing"}
		leaf := &cobra.Command{
			Use:  "go <target>",
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				onRun(
					fmt.Sprintf("name=%s count=%d loud=%v tags=%v", name, count, loud, tags),
					args,
				)
				return nil
			},
			ValidArgsFunction: func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
				return []cobra.Completion{"from-real"}, cobra.ShellCompDirectiveNoFileComp
			},
		}
		leaf.Flags().StringVarP(&name, "name", "n", "default", "a name")
		leaf.Flags().IntVar(&count, "count", 1, "a count")
		leaf.Flags().BoolVar(&loud, "loud", false, "be loud")
		leaf.Flags().StringArrayVarP(&tags, "tag", "t", nil, "repeatable")
		_ = leaf.RegisterFlagCompletionFunc(
			"name",
			func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
				return []cobra.Completion{"alice", "bob"}, cobra.ShellCompDirectiveNoFileComp
			},
		)
		root.AddCommand(leaf)
		return root
	}
}

func TestLazyCommandCopiesEveryFlagKindToTheRealCommand(t *testing.T) {
	var got string
	var gotArgs []string
	cmd := lazyCommand(
		func() (*runtime, error) { return &runtime{}, nil },
		fakeTree(func(flags string, args []string) { got, gotArgs = flags, args }),
	)
	cmd.SetArgs(
		[]string{
			"go",
			"target",
			"-n",
			"carol",
			"--count",
			"7",
			"--loud",
			"-t",
			"a,b",
			"--tag",
			"c",
		},
	)
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "name=carol count=7 loud=true tags=[a,b c]", got,
		"string, int, bool, shorthand and repeated flags must all reach the real command")
	assert.Equal(t, []string{"target"}, gotArgs)
}

func TestLazyCommandKeepsDefaultsForFlagsNotGiven(t *testing.T) {
	var got string
	cmd := lazyCommand(
		func() (*runtime, error) { return &runtime{}, nil },
		fakeTree(func(flags string, _ []string) { got = flags }),
	)
	cmd.SetArgs([]string{"go", "target"})
	cmd.SetContext(context.Background())
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "name=default count=1 loud=false tags=[]", got)
}

func TestLazyCommandDelegatesCompletionToTheRealCommand(t *testing.T) {
	cmd := lazyCommand(
		func() (*runtime, error) { return &runtime{}, nil },
		fakeTree(func(string, []string) {}),
	)
	leaf, _, err := cmd.Find([]string{"go"})
	require.NoError(t, err)

	args, _ := leaf.ValidArgsFunction(leaf, nil, "")
	assert.Equal(t, []cobra.Completion{"from-real"}, args)

	complete, ok := leaf.GetFlagCompletionFunc("name")
	require.True(t, ok, "the flag keeps a completion function")
	values, _ := complete(leaf, nil, "")
	assert.Equal(t, []cobra.Completion{"alice", "bob"}, values)
}

func TestLazyCommandCompletionFailsQuietlyWhenTheRuntimeFails(t *testing.T) {
	cmd := lazyCommand(
		func() (*runtime, error) { return nil, errors.New("runtime unavailable") },
		fakeTree(func(string, []string) {}),
	)
	leaf, _, err := cmd.Find([]string{"go"})
	require.NoError(t, err)

	args, directive := leaf.ValidArgsFunction(leaf, nil, "")
	assert.Empty(t, args)
	assert.Equal(t, cobra.ShellCompDirectiveError, directive)

	complete, _ := leaf.GetFlagCompletionFunc("name")
	values, directive := complete(leaf, nil, "")
	assert.Empty(t, values)
	assert.Equal(t, cobra.ShellCompDirectiveError, directive)
}

// If the real tree ever stops having a command the shape had, the run fails
// with a message instead of panicking.
func TestLazyCommandReportsAMissingRealSubcommand(t *testing.T) {
	builds := 0
	cmd := lazyCommand(
		func() (*runtime, error) { return &runtime{}, nil },
		func(r *runtime) *cobra.Command {
			builds++
			root := &cobra.Command{Use: "thing"}
			if builds == 1 { // the shape has a child, the real tree does not
				root.AddCommand(
					&cobra.Command{
						Use:  "gone",
						RunE: func(*cobra.Command, []string) error { return nil },
					},
				)
			}
			return root
		},
	)
	cmd.SetArgs([]string{"gone"})
	cmd.SetContext(context.Background())
	require.ErrorContains(t, cmd.Execute(), "gone subcommand not found")
}

func TestLazyCommandRunsACommandWithoutRunE(t *testing.T) {
	ran := false
	build := func(*runtime) *cobra.Command {
		return &cobra.Command{Use: "plain", Run: func(*cobra.Command, []string) { ran = true }}
	}
	cmd := lazyCommand(func() (*runtime, error) { return &runtime{}, nil }, build)
	cmd.SetContext(context.Background())
	cmd.SetArgs(nil)
	require.NoError(t, cmd.Execute())
	assert.True(t, ran)
}
