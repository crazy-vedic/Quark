package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
)

// EnvResolver is the minimum interface needed to resolve environment variables.
// *store.Store and *store.Transaction satisfy this through duck typing.
type EnvResolver interface {
	GetEnvironment(ctx context.Context, id string) (*domain.Environment, error)
	GetGlobalEnvironment(ctx context.Context) (*domain.Environment, error)
	ListCollectionEnvironments(
		ctx context.Context,
		collectionID string,
	) ([]*domain.Environment, error)
}

// ErrUnresolvedVariable is returned when a {{VAR}} placeholder has no match
// in the collection or global environment.
var ErrUnresolvedVariable = errors.New("unresolved variable")

// placeholderRE matches Postman-style {{VAR}} syntax.
var placeholderRE = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// InterpolateRequest replaces {{VAR}} placeholders in req using the provided
// environment variables. Collection variables take precedence over global variables.
// Returns a new domain.Request with substitutions applied; the input is not modified.
// Returns ErrUnresolvedVariable if any placeholder cannot be resolved.
func InterpolateRequest(
	req *domain.Request,
	colEnv, globalEnv map[string]string,
) (*domain.Request, error) {
	return InterpolateRequestWithOverrides(req, nil, nil, colEnv, globalEnv)
}

// InterpolateRequestWithOverrides extends interpolation with CLI-provided overrides.
// Positional arguments are exposed as {{1}}, {{2}}, etc. Named overrides take precedence
// over collection and global envs. Fallback syntax {{1|merchant_id}} tries candidates
// from left to right and errors only if none resolve.
func InterpolateRequestWithOverrides(
	req *domain.Request,
	positionals []string,
	named, colEnv, globalEnv map[string]string,
) (*domain.Request, error) {
	if req == nil {
		return nil, errors.New("interpolate: nil request")
	}

	hasOverrides := len(positionals) > 0 || len(named) > 0

	// Fast path: no vars provided at all, and no placeholders in the request.
	if !hasOverrides && len(colEnv) == 0 && len(globalEnv) == 0 {
		if hasPlaceholder(req.URL) || hasPlaceholder(req.Body) ||
			hasPlaceholderInHeaders(req.Headers) ||
			hasPlaceholderInAuthConfig(req.AuthConfig) {
			// Find the first unresolved variable name to include in the error.
			unresolved := firstPlaceholder(req.URL)
			if unresolved == "" {
				unresolved = firstPlaceholder(req.Body)
			}
			if unresolved == "" {
				unresolved = firstPlaceholderInHeaders(req.Headers)
			}
			if unresolved == "" {
				unresolved = firstPlaceholderInAuthConfig(req.AuthConfig)
			}
			return nil, fmt.Errorf(
				"interpolate: %w: %q (no environments active)",
				ErrUnresolvedVariable,
				unresolved,
			)
		}
		return copyRequest(req), nil
	}

	out := copyRequest(req)
	resolver := newPlaceholderResolver(positionals, named, colEnv, globalEnv)

	// URL
	var err error
	out.URL, err = substitute(out.URL, resolver)
	if err != nil {
		return nil, fmt.Errorf("interpolate URL: %w", err)
	}

	// Body
	out.Body, err = substitute(out.Body, resolver)
	if err != nil {
		return nil, fmt.Errorf("interpolate body: %w", err)
	}

	// Headers
	out.Headers, err = substituteHeaders(out.Headers, resolver)
	if err != nil {
		return nil, fmt.Errorf("interpolate headers: %w", err)
	}

	out.AuthConfig, err = substituteAuthConfig(out.AuthConfig, resolver)
	if err != nil {
		return nil, fmt.Errorf("interpolate auth config: %w", err)
	}

	return out, nil
}

type placeholderResolver func(expr string) (string, bool)

func newPlaceholderResolver(
	positionals []string,
	named, colEnv, globalEnv map[string]string,
) placeholderResolver {
	return func(expr string) (string, bool) {
		candidates := strings.Split(expr, "|")
		for i, candidate := range candidates {
			if len(candidates) > 1 || i > 0 {
				candidate = strings.TrimSpace(candidate)
			}
			if candidate == "" {
				continue
			}
			if idx, ok := parsePositionalIndex(candidate); ok {
				if idx <= len(positionals) {
					return positionals[idx-1], true
				}
				continue
			}
			if val, ok := named[candidate]; ok {
				return val, true
			}
			if val, ok := colEnv[candidate]; ok {
				return val, true
			}
			if val, ok := globalEnv[candidate]; ok {
				return val, true
			}
		}
		return "", false
	}
}

func parsePositionalIndex(candidate string) (int, bool) {
	idx, err := strconv.Atoi(candidate)
	if err != nil || idx <= 0 {
		return 0, false
	}
	return idx, true
}

// substitute replaces all {{VAR}} in s using the provided resolver.
func substitute(s string, resolve placeholderResolver) (string, error) {
	if !placeholderRE.MatchString(s) {
		return s, nil
	}

	var unresolved string
	result := placeholderRE.ReplaceAllStringFunc(s, func(match string) string {
		expr := match[2 : len(match)-2] // strip {{ and }}
		if val, ok := resolve(expr); ok {
			return val
		}

		unresolved = expr
		return match // leave unresolved placeholder as-is for now
	})

	if unresolved != "" {
		return "", fmt.Errorf("%w: %q", ErrUnresolvedVariable, unresolved)
	}

	return result, nil
}

// substituteHeaders parses the JSON header map, substitutes values, and re-serializes.
func substituteHeaders(headersJSON string, resolve placeholderResolver) (string, error) {
	if headersJSON == "" || headersJSON == "{}" {
		return headersJSON, nil
	}
	if !placeholderRE.MatchString(headersJSON) {
		return headersJSON, nil
	}

	var headers map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		// Malformed JSON — don't fail; return as-is and let the caller handle it.
		return headersJSON, nil
	}

	var unresolved string
	for key, value := range headers {
		if !placeholderRE.MatchString(value) {
			continue
		}
		newValue := placeholderRE.ReplaceAllStringFunc(value, func(match string) string {
			expr := match[2 : len(match)-2]
			if val, ok := resolve(expr); ok {
				return val
			}
			unresolved = expr
			return match
		})
		headers[key] = newValue
	}

	if unresolved != "" {
		return "", fmt.Errorf("%w: %q", ErrUnresolvedVariable, unresolved)
	}

	b, err := json.Marshal(headers)
	if err != nil {
		return "", fmt.Errorf("marshal headers: %w", err)
	}
	return string(b), nil
}

func substituteAuthConfig(authConfigJSON string, resolve placeholderResolver) (string, error) {
	if authConfigJSON == "" || authConfigJSON == "{}" {
		return authConfigJSON, nil
	}
	if !placeholderRE.MatchString(authConfigJSON) {
		return authConfigJSON, nil
	}

	var cfg map[string]string
	if err := json.Unmarshal([]byte(authConfigJSON), &cfg); err != nil {
		return authConfigJSON, nil
	}

	var unresolved string
	for key, value := range cfg {
		if !placeholderRE.MatchString(value) {
			continue
		}
		newValue := placeholderRE.ReplaceAllStringFunc(value, func(match string) string {
			expr := match[2 : len(match)-2]
			if val, ok := resolve(expr); ok {
				return val
			}
			unresolved = expr
			return match
		})
		cfg[key] = newValue
	}

	if unresolved != "" {
		return "", fmt.Errorf("%w: %q", ErrUnresolvedVariable, unresolved)
	}

	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal auth config: %w", err)
	}
	return string(b), nil
}

// hasPlaceholder reports whether s contains any {{VAR}} syntax.
func hasPlaceholder(s string) bool {
	return placeholderRE.MatchString(s)
}

// hasPlaceholderInHeaders reports whether the JSON header string contains any {{VAR}} syntax.
func hasPlaceholderInHeaders(headersJSON string) bool {
	return placeholderRE.MatchString(headersJSON)
}

func hasPlaceholderInAuthConfig(authConfigJSON string) bool {
	return placeholderRE.MatchString(authConfigJSON)
}

// firstPlaceholder extracts the first {{VAR}} variable name from s.
func firstPlaceholder(s string) string {
	match := placeholderRE.FindStringSubmatch(s)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// firstPlaceholderInHeaders extracts the first {{VAR}} variable name from a JSON header string.
func firstPlaceholderInHeaders(headersJSON string) string {
	if headersJSON == "" || headersJSON == "{}" {
		return ""
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return firstPlaceholder(headersJSON)
	}
	for _, value := range headers {
		if v := firstPlaceholder(value); v != "" {
			return v
		}
	}
	return ""
}

func firstPlaceholderInAuthConfig(authConfigJSON string) string {
	if authConfigJSON == "" || authConfigJSON == "{}" {
		return ""
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(authConfigJSON), &cfg); err != nil {
		return firstPlaceholder(authConfigJSON)
	}
	for _, value := range cfg {
		if v := firstPlaceholder(value); v != "" {
			return v
		}
	}
	return ""
}

// ResolveEnvVars resolves environment variables for a collection using the
// standard fallback chain: explicit active env → "default" named env → first
// collection env → nil. Returns (collectionEnv, globalEnv) maps. Both callers
// (TUI env panel and CLI executor) use this single implementation.
func ResolveEnvVars(
	ctx context.Context,
	r EnvResolver,
	activeEnvID, collectionID string,
) (colEnv, globalEnv map[string]string) {
	if r == nil {
		return nil, nil
	}

	// Global env.
	global, err := r.GetGlobalEnvironment(ctx)
	if err == nil {
		globalEnv = global.Vars()
	}

	// Active env by ID.
	if activeEnvID != "" {
		env, err := r.GetEnvironment(ctx, activeEnvID)
		if err == nil {
			colEnv = env.Vars()
			return colEnv, globalEnv
		}
	}

	// Fallback: prefer "default", then first env.
	envs, err := r.ListCollectionEnvironments(ctx, collectionID)
	if err == nil && len(envs) > 0 {
		for _, e := range envs {
			if e.Name == "default" {
				colEnv = e.Vars()
				return colEnv, globalEnv
			}
		}
		colEnv = envs[0].Vars()
	}

	return colEnv, globalEnv
}

// copyRequest returns a shallow copy of req. The string fields are immutable.
func copyRequest(req *domain.Request) *domain.Request {
	return &domain.Request{
		ID:           req.ID,
		CollectionID: req.CollectionID,
		Name:         req.Name,
		Method:       req.Method,
		URL:          req.URL,
		Headers:      req.Headers,
		AuthType:     req.AuthType,
		AuthConfig:   req.AuthConfig,
		Body:         req.Body,
		SortOrder:    req.SortOrder,
		Enabled:      req.Enabled,
		CreatedAt:    req.CreatedAt,
		UpdatedAt:    req.UpdatedAt,
	}
}
