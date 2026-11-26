package exec

import (
	"log/slog"
	"time"
)

type options struct {
	timeout           time.Duration
	maxResponseSize   int
	historyBodyLimit  int
	logger            *slog.Logger
	preRequestHooks   []PreRequestHook
	postResponseHooks []PostResponseHook
	variableResolver  VariableResolver
	executionWriter   ExecutionWriter
	followRedirects   bool
	maxRedirects      int
}

func defaultOptions() options {
	return options{
		timeout:          30 * time.Second,
		maxResponseSize:  10 << 20, // 10 MiB
		historyBodyLimit: DefaultHistoryBodyLimit,
		logger:           slog.Default(),
		followRedirects:  true,
		maxRedirects:     10,
	}
}

// Option configures an Executor. Use the With* functions in this package.
// This interface is sealed: only types in this package can implement it.
type Option interface{ apply(*options) }

type timeoutOption time.Duration

func (o timeoutOption) apply(opts *options) { opts.timeout = time.Duration(o) }

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) timeoutOption { return timeoutOption(d) }

type redirectsOption struct {
	follow bool
	max    int
}

func (o redirectsOption) apply(opts *options) {
	opts.followRedirects = o.follow
	opts.maxRedirects = o.max
}

// WithRedirects configures whether HTTP redirects are followed and the maximum
// number of hops. A non-positive maximum prevents following any redirect.
func WithRedirects(follow bool, max int) redirectsOption {
	return redirectsOption{follow: follow, max: max}
}

type maxRespSizeOption int

func (o maxRespSizeOption) apply(opts *options) { opts.maxResponseSize = int(o) }

// WithMaxResponseSize sets the in-memory threshold (bytes). Responses larger
// than this are streamed to a temp file and ExecuteResult.TempPath is set.
func WithMaxResponseSize(n int) maxRespSizeOption { return maxRespSizeOption(n) }

// DefaultHistoryBodyLimit is the largest response body prefix, in bytes, that
// is stored in execution history. Larger bodies are cut and marked truncated.
const DefaultHistoryBodyLimit = 1 << 20 // 1 MiB

type historyBodyLimitOption int

func (o historyBodyLimitOption) apply(opts *options) { opts.historyBodyLimit = int(o) }

// WithHistoryBodyLimit sets how many bytes of each response body are stored in
// execution history. Zero or less stores whole bodies.
func WithHistoryBodyLimit(n int) historyBodyLimitOption { return historyBodyLimitOption(n) }

type loggerOption struct{ l *slog.Logger }

func (o loggerOption) apply(opts *options) { opts.logger = o.l }

// WithLogger sets a custom slog.Logger.
func WithLogger(l *slog.Logger) loggerOption { return loggerOption{l} }

type preHooksOption struct{ hooks []PreRequestHook }

func (o preHooksOption) apply(opts *options) { opts.preRequestHooks = o.hooks }

// WithPreRequestHooks registers hooks to run before each HTTP dispatch.
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
