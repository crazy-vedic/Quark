package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/crazy-vedic/quark/internal/cli"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/store"
)

// lazyCommand returns the command tree that build produces, wired so that the
// runtime (config, store, executor) is only created when a command runs or
// completes. The commands themselves are declared once, in internal/cli;
// nothing here repeats their names, flags or help text.
//
// build is called twice over the life of a run:
//
//  1. At startup with a placeholder runtime, only to learn the shape of the
//     tree: names, flags, arguments and help. No placeholder method is ever
//     called, because every RunE and completion function is replaced below.
//  2. When a command runs or completes, with the real runtime. The flag values
//     parsed on the shape command are copied onto the matching real command,
//     which then does the work.
//
// The runtime cannot be built at startup because --config has not been parsed
// yet, and opening the store would slow down --help and shell completion.
func lazyCommand(rt func() (*runtime, error), build func(*runtime) *cobra.Command) *cobra.Command {
	shape := build(placeholderRuntime())
	bindLazy(shape, nil, rt, build)
	return shape
}

// placeholderRuntime has non-nil values so constructors can take method values
// such as st.ListCollections while the tree is being declared.
func placeholderRuntime() *runtime {
	return &runtime{st: &store.Store{}, importer: curl.NewImporter()}
}

// realCommand builds the command tree with the real runtime and returns the
// command at path (names from the root of the tree; empty means the root).
func realCommand(
	rt func() (*runtime, error),
	build func(*runtime) *cobra.Command,
	path []string,
) (*cobra.Command, error) {
	r, err := rt()
	if err != nil {
		return nil, err
	}
	target := build(r)
	for _, name := range path {
		var next *cobra.Command
		for _, child := range target.Commands() {
			if child.Name() == name {
				next = child
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("%s subcommand not found", name)
		}
		target = next
	}
	return target, nil
}

func bindLazy(
	cmd *cobra.Command,
	path []string,
	rt func() (*runtime, error),
	build func(*runtime) *cobra.Command,
) {
	for _, child := range cmd.Commands() {
		bindLazy(child, append(append([]string(nil), path...), child.Name()), rt, build)
	}

	if cmd.RunE != nil || cmd.Run != nil {
		cmd.Run = nil
		cmd.RunE = func(c *cobra.Command, args []string) error {
			target, err := realCommand(rt, build, path)
			if err != nil {
				return err
			}
			if err := copyChangedFlags(c, target); err != nil {
				return err
			}
			switch {
			case target.RunE != nil:
				return target.RunE(c, args)
			case target.Run != nil:
				target.Run(c, args)
			}
			return nil
		}
	}

	if cmd.ValidArgsFunction != nil {
		cmd.ValidArgsFunction = func(
			c *cobra.Command, args []string, toComplete string,
		) ([]cobra.Completion, cobra.ShellCompDirective) {
			target, err := realCommand(rt, build, path)
			if err != nil || target.ValidArgsFunction == nil {
				return nil, cobra.ShellCompDirectiveError
			}
			return target.ValidArgsFunction(c, args, toComplete)
		}
	}

	rebindFlagCompletions(cmd, path, rt, build)
}

// rebindFlagCompletions points flag completion at the real command. cobra keeps
// completion functions in a registry keyed by flag and has no way to replace
// one, so each such flag is swapped for a copy that gets the delegating function.
func rebindFlagCompletions(
	cmd *cobra.Command,
	path []string,
	rt func() (*runtime, error),
	build func(*runtime) *cobra.Command,
) {
	var withCompletion []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if _, ok := cmd.GetFlagCompletionFunc(f.Name); ok {
			withCompletion = append(withCompletion, f.Name)
		}
	})
	if len(withCompletion) == 0 {
		return
	}

	var flags []*pflag.Flag
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		clone := *f
		flags = append(flags, &clone)
	})
	cmd.ResetFlags()
	for _, f := range flags {
		cmd.Flags().AddFlag(f)
	}
	for _, name := range withCompletion {
		_ = cmd.RegisterFlagCompletionFunc(name, func(
			c *cobra.Command, args []string, toComplete string,
		) ([]cobra.Completion, cobra.ShellCompDirective) {
			target, err := realCommand(rt, build, path)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			fn, ok := target.GetFlagCompletionFunc(name)
			if !ok {
				return nil, cobra.ShellCompDirectiveError
			}
			return fn(c, args, toComplete)
		})
	}
}

// copyChangedFlags sets, on dst, every flag the user gave on src.
func copyChangedFlags(src, dst *cobra.Command) error {
	var err error
	src.Flags().VisitAll(func(f *pflag.Flag) {
		if err != nil || !f.Changed || dst.Flags().Lookup(f.Name) == nil {
			return
		}
		if list, ok := f.Value.(pflag.SliceValue); ok {
			for _, value := range list.GetSlice() {
				if err = dst.Flags().Set(f.Name, value); err != nil {
					return
				}
			}
			return
		}
		err = dst.Flags().Set(f.Name, f.Value.String())
	})
	return err
}

func lazyCollectionCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command { return cli.NewCollectionCmd(r.st) })
}

func lazyRequestCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command { return cli.NewRequestCmd(r.st) })
}

func lazyRunCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(
		rt,
		func(r *runtime) *cobra.Command { return cli.NewRunCmd(r.st, r.executor) },
	)
}

func lazySearchCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command { return cli.NewSearchCmd(r.st) })
}

func lazyEnvCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command { return cli.NewEnvCmd(r.st) })
}

func lazyScheduleCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command {
		return cli.NewScheduleCmd(r.st, r.executor, time.Now)
	})
}

func lazyImportCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command {
		saveCertificate := func(_ context.Context, spec *curl.CertificateSpec, rawURL string) error {
			return saveRuntimeImportedCertificate(r, spec, rawURL)
		}
		return cli.NewImportCmd(r.st, r.importer, saveCertificate)
	})
}

func lazyExportPostmanCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command { return cli.NewExportPostmanCmd(r.st) })
}

func lazyExportHARCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command {
		return cli.NewExportHARCmd(r.st, Version)
	})
}

func lazyImportPostmanCmd(rt func() (*runtime, error)) *cobra.Command {
	return lazyCommand(rt, func(r *runtime) *cobra.Command {
		return cli.NewImportPostmanCmd(r.st, cli.NewDebugLogger(nil))
	})
}
