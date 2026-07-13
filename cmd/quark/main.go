// Command quark is the TUI HTTP client.
// This file is the only place where concrete types are constructed and wired.
// All other consumers receive narrow interfaces.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/crazy-vedic/quark/internal/cli"
	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
	"github.com/crazy-vedic/quark/internal/store"
	"github.com/crazy-vedic/quark/internal/tui"
)

var (
	debugMode bool
	configDir string
	dimFlag   string
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "quark: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Signal-aware root context: Ctrl+C or SIGTERM cancels in-flight operations.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defaultConfigDir, err := defaultConfigDir()
	if err != nil {
		return err
	}
	root := &cobra.Command{
		Use:   "quark",
		Short: "A keyboard-driven TUI HTTP client",
		Long:  "Quark — local-first, keyboard-driven HTTP client. No cloud dependencies.",
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetContext(ctx)
	root.PersistentFlags().
		BoolVar(&debugMode, "debug", false, "Log keystrokes and diagnostics to /tmp/quark_debug_logs/debug.log")
	root.PersistentFlags().
		StringVar(&configDir, "config", defaultConfigDir, "Directory for config and db")
	root.PersistentFlags().
		StringVar(&dimFlag, "dim", "", "Force TUI density: wide|narrow|tiny|absurd (default: auto from terminal size)")

	var debugLog *os.File
	defer func() {
		if debugLog != nil {
			_ = debugLog.Close()
		}
	}()

	logger := cli.NewDebugLogger(nil)

	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if debugMode && debugLog == nil {
			debugLog = openDebugLog()
			logger.SetFile(debugLog)
		}
	}

	// Lazy store & config — initialised on first use so the --config flag is honoured.
	var rt *runtime
	runtimeOnce := func() (*runtime, error) {
		if rt != nil {
			return rt, nil
		}
		// Load user config (defaults if file absent or unreadable).
		cfg, err := config.Load(configDir)
		if err != nil {
			slog.Warn("config: failed to load config.toml; using defaults", "err", err)
			cfg = config.Default(configDir)
		}

		// 0700: only the owner can read the directory. It contains the SQLite DB
		// (which stores Authorization headers and cookies) and backup copies.
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			return nil, fmt.Errorf("create config dir: %w", err)
		}

		dbPath := filepath.Join(configDir, "quark.db")
		backupDir := cfg.BackupDir(configDir)

		storeOpts := []store.Option{store.WithBackup(backupDir)}
		if !cfg.Backup.AutoBackup {
			storeOpts = []store.Option{}
		}
		st, err := store.New(dbPath, storeOpts...)
		if err != nil {
			return nil, fmt.Errorf("open store: %w", err)
		}

		transport := &http.Transport{
			ResponseHeaderTimeout: cfg.Timeout(),
		}
		executor := exec.New(transport,
			exec.WithTimeout(cfg.Timeout()),
			exec.WithVariableResolver(makeVariableResolver(st)),
			exec.WithExecutionWriter(st),
		)
		importer := curl.NewImporter()
		searcher := search.New(st)

		rt = &runtime{
			st:       st,
			cfg:      cfg,
			executor: executor,
			importer: importer,
			searcher: searcher,
		}
		return rt, nil
	}

	// Subcommands that need the store are registered here.
	// Each command lazily initialises the runtime on the first invocation.
	root.AddCommand(cli.NewWarpCompletionPluginCmd())
	root.AddCommand(lazyCollectionCmd(runtimeOnce))
	root.AddCommand(lazyRequestCmd(runtimeOnce))
	root.AddCommand(lazyRunCmd(runtimeOnce))
	root.AddCommand(lazySearchCmd(runtimeOnce))
	root.AddCommand(lazyScheduleCmd(runtimeOnce))
	root.AddCommand(lazyImportCmd(runtimeOnce))
	root.AddCommand(lazyImportPostmanCmd(runtimeOnce))
	root.AddCommand(lazyEnvCmd(runtimeOnce))
	root.AddCommand(lazyKeybindingsCmd())
	root.AddCommand(cli.NewCompletionCmd(root))

	root.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := runtimeOnce()
			if err != nil {
				return err
			}
			return launchTUI(
				ctx, rt.st, rt.executor, rt.searcher, rt.importer, rt.cfg, debugLog, configDir,
			)
		},
	})

	// Default: launch TUI when no subcommand is given.
	// cobra.NoArgs ensures unknown positional arguments produce an error
	// instead of silently succeeding.
	root.Args = cobra.NoArgs
	root.RunE = func(cmd *cobra.Command, args []string) error {
		rt, err := runtimeOnce()
		if err != nil {
			return err
		}
		return launchTUI(
			ctx,
			rt.st,
			rt.executor,
			rt.searcher,
			rt.importer,
			rt.cfg,
			debugLog,
			configDir,
		)
	}

	return root.Execute()
}

// runtime holds lazily-initialised dependencies that depend on the --config flag.
type runtime struct {
	st       *store.Store
	cfg      config.Config
	executor *exec.Executor
	importer *curl.Importer
	searcher *search.Searcher
}

// openDebugLog opens /tmp/quark_debug_logs/debug.log, archiving any existing file.
// The first line of the new log is the current epoch time.
// Returns nil on error (debug logging is best-effort).
func openDebugLog() *os.File {
	debugDir := filepath.Join("/tmp", "quark_debug_logs")
	if err := os.MkdirAll(debugDir, 0o700); err != nil {
		return nil
	}
	latestPath := filepath.Join(debugDir, "debug.log")
	if info, err := os.Stat(latestPath); err == nil && !info.IsDir() {
		epoch := strconv.FormatInt(info.ModTime().Unix(), 10)
		archivePath := filepath.Join(debugDir, fmt.Sprintf("debug_%s.log", epoch))
		_ = os.Rename(latestPath, archivePath)
	}
	f, err := os.OpenFile(latestPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	_, _ = fmt.Fprintf(f, "epoch: %d\n", time.Now().Unix())
	return f
}

// launchTUI constructs the TUI model and runs the bubbletea program.
// bubbletea v1.3.10 catches panics by default and restores the terminal
// before returning — no explicit panic recovery is needed here.
func launchTUI(
	ctx context.Context,
	st *store.Store,
	executor *exec.Executor,
	searcher *search.Searcher,
	importer *curl.Importer,
	cfg config.Config,
	debugLog *os.File,
	configDir string,
) error {
	forceDim := tui.DimAuto
	if dimFlag != "" {
		parsed, err := tui.ParseDimMode(dimFlag)
		if err != nil {
			return err
		}
		forceDim = parsed
	}
	model := tui.New(tui.Deps{
		Lister:          st,
		Reader:          st,
		Writer:          st,
		ColWriter:       st,
		ExecutionReader: st,
		Executor:        executor,
		Searcher:        searcher,
		Importer:        importer,
		EnvReader:       st,
		EnvWriter:       st,
		ActiveEnvStore:  st,
		Scheduler:       st,
		Config:          cfg,
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
		Ctx:             ctx, // signal-aware context: TUI goroutines cancel on SIGINT/SIGTERM
		DebugLog:        debugLog,
		ConfigDir:       configDir,
		ForceDim:        forceDim,
	})

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func defaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".quark"), nil
}

// lazy wrapper functions create cobra commands that lazily initialise the runtime.

func lazyCollectionCmd(rt func() (*runtime, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collection",
		Short: "Manage collections",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all collections",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			for _, c := range cli.NewCollectionCmd(r.st).Commands() {
				if c.Name() == "list" {
					return c.RunE(cmd, args)
				}
			}
			return fmt.Errorf("list subcommand not found")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a new collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			for _, c := range cli.NewCollectionCmd(r.st).Commands() {
				if c.Name() == "create" {
					return c.RunE(cmd, args)
				}
			}
			return fmt.Errorf("create subcommand not found")
		},
	})
	return cmd
}

func lazyRequestCmd(rt func() (*runtime, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Manage requests",
	}
	var collectionID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List requests in a collection",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			for _, c := range cli.NewRequestCmd(r.st).Commands() {
				if c.Name() == "list" {
					if err := c.Flags().Set("collection", collectionID); err != nil {
						return err
					}
					return c.RunE(c, args)
				}
			}
			return fmt.Errorf("list subcommand not found")
		},
	}
	listCmd.Flags().StringVar(&collectionID, "collection", "", "Collection ID")
	_ = listCmd.RegisterFlagCompletionFunc("collection", cli.CompleteCollectionIDs(
		func(ctx context.Context) ([]*domain.Collection, error) {
			r, err := rt()
			if err != nil {
				return nil, err
			}
			return r.st.ListCollections(ctx)
		},
	))
	cmd.AddCommand(listCmd)
	return cmd
}

func lazyRunCmd(rt func() (*runtime, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <Collection/Request>",
		Short: "Execute a saved request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			return cli.NewRunCmd(r.st, r.executor).RunE(cmd, args)
		},
	}
	cmd.ValidArgsFunction = cli.CompleteRequestPaths(
		func(ctx context.Context) ([]*domain.Collection, error) {
			r, err := rt()
			if err != nil {
				return nil, err
			}
			return r.st.ListCollections(ctx)
		},
		func(ctx context.Context, collectionID string) ([]*domain.Request, error) {
			r, err := rt()
			if err != nil {
				return nil, err
			}
			return r.st.ListRequests(ctx, collectionID)
		},
	)
	return cmd
}

func lazySearchCmd(rt func() (*runtime, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search saved requests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			return cli.NewSearchCmd(r.st).RunE(cmd, args)
		},
	}
}

func lazyScheduleCmd(rt func() (*runtime, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Schedule delayed request execution",
	}
	var when string
	addCmd := &cobra.Command{
		Use:   "add <Collection/Request>",
		Short: "Schedule a saved request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			inner := cli.NewScheduleCmd(r.st, r.executor, time.Now)
			inner.SetContext(cmd.Context())
			inner.SetOut(cmd.OutOrStdout())
			inner.SetErr(cmd.ErrOrStderr())
			inner.SetArgs([]string{"add", args[0], "--at", when})
			return inner.Execute()
		},
	}
	addCmd.ValidArgsFunction = cli.CompleteRequestPaths(
		func(ctx context.Context) ([]*domain.Collection, error) {
			r, err := rt()
			if err != nil {
				return nil, err
			}
			return r.st.ListCollections(ctx)
		},
		func(ctx context.Context, collectionID string) ([]*domain.Request, error) {
			r, err := rt()
			if err != nil {
				return nil, err
			}
			return r.st.ListRequests(ctx, collectionID)
		},
	)
	addCmd.Flags().
		StringVar(&when, "at", "", "Run time: duration, 'in 10m', RFC3339, or 'YYYY-MM-DD HH:MM'")
	cmd.AddCommand(addCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List scheduled runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			inner := cli.NewScheduleCmd(r.st, r.executor, time.Now)
			inner.SetContext(cmd.Context())
			inner.SetOut(cmd.OutOrStdout())
			inner.SetErr(cmd.ErrOrStderr())
			inner.SetArgs([]string{"list"})
			return inner.Execute()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "run-due",
		Short: "Execute all pending scheduled runs due now",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			inner := cli.NewScheduleCmd(r.st, r.executor, time.Now)
			inner.SetContext(cmd.Context())
			inner.SetOut(cmd.OutOrStdout())
			inner.SetErr(cmd.ErrOrStderr())
			inner.SetArgs([]string{"run-due"})
			return inner.Execute()
		},
	})
	return cmd
}

func lazyImportCmd(rt func() (*runtime, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "import",
		Short: "Import requests from external formats",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			return cli.NewImportCmd(r.st, r.importer).RunE(cmd, args)
		},
	}
}

func lazyImportPostmanCmd(rt func() (*runtime, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "import-postman <collection.json|directory>",
		Short: "Import a Postman Collection v2.1 JSON file or a bulk export directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			return cli.NewImportPostmanCmd(r.st, cli.NewDebugLogger(nil)).RunE(cmd, args)
		},
	}
}

func lazyEnvCmd(rt func() (*runtime, error)) *cobra.Command {
	root := cli.NewEnvCmd(nil)
	// Wrap each subcommand so it resolves the runtime lazily and delegates.
	for _, sub := range root.Commands() {
		c := sub // capture for closure
		root.RemoveCommand(c)
		wrapped := *c
		wrapped.RunE = func(cmd *cobra.Command, args []string) error {
			r, err := rt()
			if err != nil {
				return err
			}
			for _, s := range cli.NewEnvCmd(r.st).Commands() {
				if s.Name() == c.Name() {
					return s.RunE(cmd, args)
				}
			}
			return fmt.Errorf("%s subcommand not found", c.Name())
		}
		switch c.Name() {
		case "list", "create":
			wrapped.ValidArgsFunction = cli.CompleteCollectionIDs(
				func(ctx context.Context) ([]*domain.Collection, error) {
					r, err := rt()
					if err != nil {
						return nil, err
					}
					return r.st.ListCollections(ctx)
				},
			)
		case "set", "delete", "active":
			wrapped.ValidArgsFunction = cli.CompleteCollectionThenEnvironment(
				func(ctx context.Context) ([]*domain.Collection, error) {
					r, err := rt()
					if err != nil {
						return nil, err
					}
					return r.st.ListCollections(ctx)
				},
				func(ctx context.Context, collectionID string) ([]*domain.Environment, error) {
					r, err := rt()
					if err != nil {
						return nil, err
					}
					return r.st.ListCollectionEnvironments(ctx, collectionID)
				},
			)
		}
		root.AddCommand(&wrapped)
	}
	return root
}

func lazyKeybindingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keybindings",
		Short: "Manage keybindings",
	}
	// list
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Show current keybindings",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, c := range cli.NewKeybindingsCmd(configDir).Commands() {
				if c.Name() == "list" {
					return c.RunE(cmd, args)
				}
			}
			return fmt.Errorf("list subcommand not found")
		},
	})
	// set
	cmd.AddCommand(&cobra.Command{
		Use:   "set <action> <key>",
		Short: "Set a keybinding",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// cobra.Command.Commands() returns subcommands in alphabetical order,
			// not insertion order. Look up the set command by name explicitly.
			for _, c := range cli.NewKeybindingsCmd(configDir).Commands() {
				if c.Name() == "set" {
					return c.RunE(cmd, args)
				}
			}
			return fmt.Errorf("set subcommand not found")
		},
	})
	// reset
	cmd.AddCommand(&cobra.Command{
		Use:   "reset",
		Short: "Reset all keybindings to defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, c := range cli.NewKeybindingsCmd(configDir).Commands() {
				if c.Name() == "reset" {
					return c.RunE(cmd, args)
				}
			}
			return fmt.Errorf("reset subcommand not found")
		},
	})
	return cmd
}

// makeVariableResolver returns a VariableResolver that looks up environments
// from the store. The active environment is the "default" env if present,
// otherwise the first collection environment. Global environment is the
// fallback for variables not found in the collection env.
func makeVariableResolver(st *store.Store) exec.VariableResolver {
	return func(collectionID string) (colEnv, globalEnv map[string]string) {
		ctx, cancel := context.WithTimeout(context.Background(), store.EnvDBTimeout)
		defer cancel()

		// Load the persisted active env for this collection (if any).
		activeEnvID, err := st.GetActiveEnvironment(ctx, collectionID)
		if err != nil {
			activeEnvID = ""
		}
		return exec.ResolveEnvVars(ctx, st, activeEnvID, collectionID)
	}
}
