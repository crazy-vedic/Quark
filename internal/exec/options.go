package exec

import (
	"log/slog"
	"time"
)

type options struct {
	timeout           time.Duration
	maxResponseSize   int
	logger            *slog.Logger
	preRequestHooks   []PreRequestHook
	postResponseHooks []PostResponseHook
	variableResolver  VariableResolver
	executionWriter   ExecutionWriter
}

func defaultOptions() options {
	return options{
		timeout:         30 * time.Second,
		maxResponseSize: 10 << 20, // 10 MiB
		logger:          slog.Default(),
	}
}

// Option configures an Executor. Use the With* functions in this package.
// This interface is sealed: only types in this package can implement it.
type Option interface{ apply(*options) }

type timeoutOption time.Duration

func (o timeoutOption) apply(opts *options) { opts.timeout = time.Duration(o) }

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) timeoutOption { return timeoutOption(d) }

type maxRespSizeOption int

func (o maxRespSizeOption) apply(opts *options) { opts.maxResponseSize = int(o) }

// WithMaxResponseSize sets the in-memory threshold (bytes). Responses larger
// than this are streamed to a temp file and ExecuteResult.TempPath is set.
func WithMaxResponseSize(n int) maxRespSizeOption { return maxRespSizeOption(n) }

type loggerOption struct{ l *slog.Logger }

func (o loggerOption) apply(opts *options) { opts.logger = o.l }

// WithLogger sets a custom slog.Logger.
func WithLogger(l *slog.Logger) loggerOption { return loggerOption{l} }

type preHooksOption struct{ hooks []PreRequestHook }

func (o preHooksOption) apply(opts *options) { opts.preRequestHooks = o.hooks }

// WithPreRequestHooks registers hooks to run before each HTTP dispatch.
// Any type satisfying exec.PreRequestHook (including plugin.PreRequestHook values)
// can be passed; Go structural typing handles the conversion at the call site.
func WithPreRequestHooks(hooks []PreRequestHook) preHooksOption { return preHooksOption{hooks} }

type postHooksOption struct{ hooks []PostResponseHook }

func (o postHooksOption) apply(opts *options) { opts.postResponseHooks = o.hooks }

// WithPostResponseHooks registers hooks to run after each HTTP response is received.
func WithPostResponseHooks(
	hooks []PostResponseHook,
) postHooksOption {
	return postHooksOption{hooks}
}

type variableResolverOption struct{ r VariableResolver }

func (o variableResolverOption) apply(opts *options) { opts.variableResolver = o.r }

// WithVariableResolver sets a resolver that maps collection IDs to environment variables.
// The resolver is called during Execute to substitute {{VAR}} placeholders in
// URL, Body, and Headers before the HTTP request is built.
func WithVariableResolver(r VariableResolver) variableResolverOption {
	return variableResolverOption{r}
}

type executionWriterOption struct{ w ExecutionWriter }

func (o executionWriterOption) apply(opts *options) { opts.executionWriter = o.w }

// WithExecutionWriter enables persistence of immutable execution history.
func WithExecutionWriter(w ExecutionWriter) executionWriterOption {
	return executionWriterOption{w}
}
