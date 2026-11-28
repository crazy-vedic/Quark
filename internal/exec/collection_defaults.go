package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ApplyCollectionDefaults returns a copy of req with the default headers and
// auth of its collection chain applied. Callers run it before variable
// interpolation, so {{VAR}} placeholders in inherited values resolve like the
// request's own. req itself is not modified.
func ApplyCollectionDefaults(
	ctx context.Context,
	r EnvResolver,
	req *domain.Request,
) (*domain.Request, error) {
	if req == nil || req.CollectionID == "" {
		return req, nil
	}
	scopes, err := LoadEnvironmentHierarchy(ctx, r, req.CollectionID)
	if err != nil {
		return nil, err
	}
	chain := make([]*domain.Collection, len(scopes))
	for i := range scopes {
		chain[i] = scopes[i].Collection
	}
	return mergeCollectionDefaults(req, chain)
}

// mergeCollectionDefaults applies chain, ordered root to leaf, to req.
//
// Headers: a child collection replaces a parent's value for the same name,
// and the request's own headers replace any name they set.
//
// Auth: a request with the inherit type takes the auth of the nearest
// collection that sets one. A collection with an empty or inherit type defers
// to its parent; a collection with none stops inheritance with no auth. An
// inherited auth yields to an Authorization header the request sets itself.
func mergeCollectionDefaults(
	req *domain.Request,
	chain []*domain.Collection,
) (*domain.Request, error) {
	out := *req

	headers, changed, err := mergeHeaderDefaults(req.Headers, chain)
	if err != nil {
		return nil, err
	}
	if changed {
		out.Headers = headers
	}

	if domain.NormalizeAuthType(req.AuthType) == domain.AuthTypeInherit {
		authType, authConfig := inheritedAuth(chain)
		own, err := parseHeadersOrEmpty(req.Headers)
		if err != nil {
			return nil, err
		}
		if own.Get("Authorization") != "" {
			authType, authConfig = domain.AuthTypeNone, ""
		}
		out.AuthType = authType
		out.AuthConfig = authConfig
	}
	return &out, nil
}

// mergeHeaderDefaults returns the request headers JSON with the collection
// defaults merged in. The bool reports whether any collection set headers; when
// none did, requestHeaders is returned as stored.
func mergeHeaderDefaults(
	requestHeaders string,
	chain []*domain.Collection,
) (string, bool, error) {
	inherited := make(http.Header)
	for _, c := range chain {
		if strings.TrimSpace(c.Headers) == "" {
			continue
		}
		defaults, err := parseHeaders(c.Headers)
		if err != nil {
			return "", false, fmt.Errorf("collection %q default headers: %w", c.Name, err)
		}
		for key, values := range defaults {
			inherited[key] = append([]string(nil), values...)
		}
	}
	if len(inherited) == 0 {
		return requestHeaders, false, nil
	}

	own, err := parseHeadersOrEmpty(requestHeaders)
	if err != nil {
		return "", false, err
	}
	for key, values := range own {
		inherited[key] = append([]string(nil), values...)
	}
	encoded, err := json.Marshal(map[string][]string(inherited))
	if err != nil {
		return "", false, fmt.Errorf("encode merged headers: %w", err)
	}
	return string(encoded), true, nil
}

// inheritedAuth returns the auth of the nearest collection, leaf first, that
// sets one. With none, it returns no auth.
func inheritedAuth(chain []*domain.Collection) (authType, authConfig string) {
	for i := len(chain) - 1; i >= 0; i-- {
		c := chain[i]
		// "none" and "" are the same auth type, so check the raw value first.
		raw := strings.ToLower(strings.TrimSpace(c.AuthType))
		if raw == "" || raw == domain.AuthTypeInherit {
			continue
		}
		if raw == "none" {
			return domain.AuthTypeNone, ""
		}
		authConfig := c.AuthConfig
		if strings.TrimSpace(authConfig) == "" {
			authConfig = "{}"
		}
		return domain.NormalizeAuthType(c.AuthType), authConfig
	}
	return domain.AuthTypeNone, ""
}

func parseHeadersOrEmpty(raw string) (http.Header, error) {
	if strings.TrimSpace(raw) == "" {
		return make(http.Header), nil
	}
	return parseHeaders(raw)
}
