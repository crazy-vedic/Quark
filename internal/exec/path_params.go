package exec

import (
	"errors"
	"fmt"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ApplyPathParams fills the URL path placeholders (such as :id) of req with
// the values stored on the request. It runs before {{variables}} are
// interpolated, so a value may use them. A request whose URL has no
// placeholders is returned unchanged, and its stored params are not parsed.
// Stored params that do not parse are an error, so the request is not sent
// with the wrong URL.
func ApplyPathParams(req *domain.Request) (*domain.Request, error) {
	if req == nil {
		return nil, errors.New("apply path params: nil request")
	}
	if len(domain.PathPlaceholders(req.URL)) == 0 {
		return req, nil
	}
	params, err := domain.ParseRequestParams(req.Params)
	if err != nil {
		return nil, fmt.Errorf("stored path params are not valid JSON: %w", err)
	}
	out := copyRequest(req)
	out.URL = domain.ApplyPathParams(req.URL, params.PathValues)
	return out, nil
}
