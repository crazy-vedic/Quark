package exec

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ErrUnsupportedOption is returned when a request asks for a proxy, TLS or
// timeout override that the executor's transport cannot apply.
var ErrUnsupportedOption = errors.New("exec: request option not supported by this transport")

// variantTransport is implemented by transports that can serve a request with
// its own proxy, TLS verification and response-header timeout while keeping
// their per-host certificate routing. transport.Manager implements it.
type variantTransport interface {
	Variant(
		proxy *url.URL,
		skipTLSVerify bool,
		responseHeaderTimeout time.Duration,
	) http.RoundTripper
}

// requestSettings are the effective settings for one request: its options
// merged over the executor's configured defaults.
type requestSettings struct {
	timeout      time.Duration
	follow       bool
	maxRedirects int
	proxy        *url.URL
	insecure     bool
}

func (e *Executor) effectiveSettings(opts domain.RequestOptions) (requestSettings, error) {
	s := requestSettings{
		timeout:      e.timeout,
		follow:       e.followRedirects,
		maxRedirects: e.maxRedirects,
		insecure:     opts.SkipTLSVerify,
	}
	if opts.TimeoutMS > 0 {
		s.timeout = opts.Timeout()
	}
	if opts.FollowRedirects != nil {
		s.follow = *opts.FollowRedirects
	}
	if opts.MaxRedirects != nil {
		s.maxRedirects = *opts.MaxRedirects
	}
	if opts.Proxy != "" {
		raw, err := domain.NormalizeProxyURL(opts.Proxy)
		if err != nil {
			return requestSettings{}, err
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			return requestSettings{}, fmt.Errorf(
				"%w: proxy URL: %w", domain.ErrInvalidRequestOptions, err,
			)
		}
		s.proxy = parsed
	}
	return s, nil
}

// clientFor returns the client for one request. Requests whose settings match
// the executor's defaults share the default client and its connection pool.
// Others get their own client, and a transport variant when the proxy, TLS or
// timeout differ. The returned release func must run after the response body
// has been read.
func (e *Executor) clientFor(
	s requestSettings,
	timeoutOverridden bool,
) (*http.Client, func(), error) {
	noRelease := func() {}
	needsTransport := s.proxy != nil || s.insecure || timeoutOverridden
	if !needsTransport &&
		s.follow == e.followRedirects &&
		s.maxRedirects == e.maxRedirects {
		return e.client, noRelease, nil
	}
	rt, release := e.transport, noRelease
	if needsTransport {
		var err error
		rt, release, err = e.roundTripperFor(s, timeoutOverridden)
		if err != nil {
			return nil, noRelease, err
		}
	}
	client := &http.Client{
		Transport:     rt,
		CheckRedirect: redirectPolicy(s.follow, s.maxRedirects),
	}
	return client, release, nil
}

func (e *Executor) roundTripperFor(
	s requestSettings,
	timeoutOverridden bool,
) (http.RoundTripper, func(), error) {
	noRelease := func() {}
	if v, ok := e.transport.(variantTransport); ok {
		// The variant owns its connection pool and keeps it between requests.
		return v.Variant(s.proxy, s.insecure, s.timeout), noRelease, nil
	}
	base, ok := e.transport.(*http.Transport)
	if !ok {
		return nil, noRelease, fmt.Errorf(
			"%w: proxy, TLS and timeout overrides need the HTTP transport",
			ErrUnsupportedOption,
		)
	}
	clone := base.Clone()
	if s.proxy != nil {
		clone.Proxy = http.ProxyURL(s.proxy)
	}
	if s.insecure {
		clone.TLSClientConfig = insecureTLSConfig(clone.TLSClientConfig)
	}
	if timeoutOverridden {
		clone.ResponseHeaderTimeout = s.timeout
	}
	// A plain transport clone has no shared pool, so its idle sockets are
	// closed once the request is done.
	return clone, clone.CloseIdleConnections, nil
}

// insecureTLSConfig turns off certificate verification on a copy of cfg. It
// runs only when the user has set skip_tls_verify on the request, like curl -k.
func insecureTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		cfg = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		cfg = cfg.Clone()
	}
	cfg.InsecureSkipVerify = true //nolint:gosec // requested per request by the user
	return cfg
}

// redirectPolicy follows redirects when enabled, up to maxRedirects hops.
func redirectPolicy(follow bool, maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(_ *http.Request, via []*http.Request) error {
		if !follow || maxRedirects <= 0 || len(via) > maxRedirects {
			return http.ErrUseLastResponse
		}
		return nil
	}
}
