package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ErrInvalidRequestOptions is wrapped by every reason a RequestOptions value
// is rejected. Use errors.Is to classify it.
var ErrInvalidRequestOptions = errors.New("invalid request options")

// RequestOptions are per-request HTTP settings. Every zero value means "use
// the configured default", so a request that sets no options behaves exactly
// as it did before options existed.
type RequestOptions struct {
	// TimeoutMS replaces the configured timeout, in milliseconds. Zero keeps it.
	TimeoutMS int `json:"timeout_ms,omitempty"`
	// FollowRedirects replaces [http] follow_redirects when non-nil.
	FollowRedirects *bool `json:"follow_redirects,omitempty"`
	// MaxRedirects replaces [http] max_redirects when non-nil.
	MaxRedirects *int `json:"max_redirects,omitempty"`
	// Proxy is an absolute proxy URL with an http, https, socks5 or socks5h
	// scheme. Empty keeps the environment proxy settings.
	Proxy string `json:"proxy,omitempty"`
	// SkipTLSVerify turns off certificate verification for this request only.
	SkipTLSVerify bool `json:"skip_tls_verify,omitempty"`
}

// Timeout returns the per-request timeout, or zero when none is set.
func (o RequestOptions) Timeout() time.Duration {
	return time.Duration(o.TimeoutMS) * time.Millisecond
}

// IsZero reports whether every option keeps its default.
func (o RequestOptions) IsZero() bool {
	return o.TimeoutMS == 0 &&
		o.FollowRedirects == nil &&
		o.MaxRedirects == nil &&
		o.Proxy == "" &&
		!o.SkipTLSVerify
}

// Summary describes the options that are set, for listings and previews. It
// returns "" when every option keeps its default.
func (o RequestOptions) Summary() string {
	var parts []string
	if o.TimeoutMS > 0 {
		parts = append(parts, "timeout "+o.Timeout().String())
	}
	if o.FollowRedirects != nil {
		if *o.FollowRedirects {
			parts = append(parts, "follow redirects")
		} else {
			parts = append(parts, "no redirects")
		}
	}
	if o.MaxRedirects != nil {
		parts = append(parts, fmt.Sprintf("max %d redirects", *o.MaxRedirects))
	}
	if o.Proxy != "" {
		parts = append(parts, "proxy "+RedactProxyURL(o.Proxy))
	}
	if o.SkipTLSVerify {
		parts = append(parts, "TLS verification off")
	}
	return strings.Join(parts, ", ")
}

// RedactProxyURL returns a proxy URL that is safe to show on screen: the
// password in user:password@host is replaced. The stored value is unchanged.
func RedactProxyURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "(invalid proxy URL)"
	}
	return parsed.Redacted()
}

// Validate reports whether the options can be applied. It does not check the
// configured defaults, which are not known here.
func (o RequestOptions) Validate() error {
	if o.TimeoutMS < 0 {
		return fmt.Errorf("%w: timeout must not be negative", ErrInvalidRequestOptions)
	}
	if o.MaxRedirects != nil && *o.MaxRedirects < 0 {
		return fmt.Errorf("%w: max redirects must not be negative", ErrInvalidRequestOptions)
	}
	if o.Proxy != "" {
		if _, err := NormalizeProxyURL(o.Proxy); err != nil {
			return err
		}
	}
	return nil
}

// JSON encodes the options for storage. A zero value encodes as "{}".
func (o RequestOptions) JSON() (string, error) {
	if o.IsZero() {
		return "{}", nil
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// ParseRequestOptions decodes stored options. Blank input and "{}" yield the
// zero value. Unknown fields are rejected: an option this build does not know
// would otherwise be dropped silently and the request sent differently.
func ParseRequestOptions(raw string) (RequestOptions, error) {
	var opts RequestOptions
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return opts, nil
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&opts); err != nil {
		return RequestOptions{}, fmt.Errorf("%w: %w", ErrInvalidRequestOptions, err)
	}
	if err := opts.Validate(); err != nil {
		return RequestOptions{}, err
	}
	return opts, nil
}

// NormalizeProxyURL returns the proxy URL to store. A bare host:port, as curl
// accepts for -x, is given the http scheme.
func NormalizeProxyURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: proxy URL: %w", ErrInvalidRequestOptions, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return "", fmt.Errorf(
			"%w: proxy scheme %q is not supported (use http, https, socks5 or socks5h)",
			ErrInvalidRequestOptions, parsed.Scheme,
		)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%w: proxy URL has no host", ErrInvalidRequestOptions)
	}
	return value, nil
}
