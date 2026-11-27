package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/jsonpath"
)

// ErrExtractionSkipped means extraction did not run because the response
// status was not 2xx.
var ErrExtractionSkipped = errors.New("extraction skipped")

// maxExtractBodySize bounds how much of a streamed body is read for
// extraction.
const maxExtractBodySize = 64 << 20

// ExtractionOutcome reports what one extraction rule did.
type ExtractionOutcome struct {
	Variable    string
	Environment string // name of the environment written to
	Err         error  // nil when the variable was written
}

// ExtractionEnvStore is the environment access ExtractionHook needs.
type ExtractionEnvStore interface {
	GetGlobalEnvironment(ctx context.Context) (*domain.Environment, error)
	GetEnvironment(ctx context.Context, id string) (*domain.Environment, error)
	ListEnvironments(ctx context.Context, collectionID string) ([]*domain.Environment, error)
	GetActiveEnvironment(ctx context.Context, collectionID string) (string, error)
	SaveEnvironment(ctx context.Context, env *domain.Environment) error
}

// ExtractionHook is a PostResponseHook that applies a request's extraction
// rules after a 2xx response. Each rule reads a JSONPath from the body or a
// header and writes the value into the request collection's active
// environment (its default environment when none is active; the global
// environment for a request with no collection). Outcomes are appended to
// ExecuteResult.Extracted; the hook itself never fails the request.
type ExtractionHook struct {
	store ExtractionEnvStore
}

// NewExtractionHook returns a hook that writes extracted values through st.
func NewExtractionHook(st ExtractionEnvStore) *ExtractionHook {
	return &ExtractionHook{store: st}
}

var _ PostResponseHook = (*ExtractionHook)(nil)

// AfterResponse implements PostResponseHook.
func (h *ExtractionHook) AfterResponse(
	ctx context.Context,
	req *domain.Request,
	res *ExecuteResult,
) error {
	if req == nil || res == nil {
		return nil
	}
	rules, err := domain.ParseExtractions(req.Extractions)
	if err != nil {
		res.Extracted = append(res.Extracted, ExtractionOutcome{Err: err})
		return nil
	}
	if len(rules) == 0 {
		return nil
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		for _, rule := range rules {
			res.Extracted = append(res.Extracted, ExtractionOutcome{
				Variable: rule.Variable,
				Err: fmt.Errorf(
					"%w: HTTP %d is not a 2xx response",
					ErrExtractionSkipped,
					res.StatusCode,
				),
			})
		}
		return nil
	}

	values := make(map[string]string, len(rules))
	outcomes := make([]ExtractionOutcome, len(rules))
	var doc any
	var docErr error
	docLoaded := false
	for i, rule := range rules {
		outcomes[i].Variable = rule.Variable
		var value string
		var ruleErr error
		switch rule.Source {
		case domain.ExtractFromHeader:
			vals := http.Header(res.Headers).Values(rule.Path)
			if len(vals) == 0 {
				ruleErr = fmt.Errorf("response has no %q header", rule.Path)
			} else {
				value = strings.Join(vals, ", ")
			}
		case domain.ExtractFromBody, "":
			if !docLoaded {
				docLoaded = true
				doc, docErr = decodeResultBody(res)
			}
			if docErr != nil {
				ruleErr = docErr
				break
			}
			var path jsonpath.Path
			path, ruleErr = jsonpath.Parse(rule.Path)
			if ruleErr != nil {
				break
			}
			var v any
			v, ruleErr = path.Lookup(doc)
			value = jsonpath.Text(v)
		default:
			ruleErr = fmt.Errorf("unknown extraction source %q", rule.Source)
		}
		if ruleErr != nil {
			outcomes[i].Err = ruleErr
			continue
		}
		values[rule.Variable] = value
	}

	if len(values) > 0 {
		env, err := h.targetEnvironment(ctx, req.CollectionID)
		if err == nil {
			err = writeVars(ctx, h.store, env, values)
		}
		for i := range outcomes {
			if outcomes[i].Err != nil {
				continue
			}
			if err != nil {
				outcomes[i].Err = fmt.Errorf("save environment: %w", err)
				continue
			}
			outcomes[i].Environment = env.Name
		}
	}
	res.Extracted = append(res.Extracted, outcomes...)
	return nil
}

// targetEnvironment returns the environment extracted values are written to.
func (h *ExtractionHook) targetEnvironment(
	ctx context.Context,
	collectionID string,
) (*domain.Environment, error) {
	if collectionID == "" {
		return h.store.GetGlobalEnvironment(ctx)
	}
	activeID, err := h.store.GetActiveEnvironment(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	if activeID != "" {
		return h.store.GetEnvironment(ctx, activeID)
	}
	envs, err := h.store.ListEnvironments(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	for _, env := range envs {
		if env.Name == "default" {
			return env, nil
		}
	}
	return nil, fmt.Errorf("%w: collection %q", ErrMissingDefaultEnvironment, collectionID)
}

func writeVars(
	ctx context.Context,
	st ExtractionEnvStore,
	env *domain.Environment,
	values map[string]string,
) error {
	vars, err := env.DecodeVars()
	if err != nil {
		return err
	}
	for k, v := range values {
		vars[k] = v
	}
	env.SetVars(vars)
	return st.SaveEnvironment(ctx, env)
}

// decodeResultBody parses the response body as JSON, reading a streamed body
// from its temp file.
func decodeResultBody(res *ExecuteResult) (any, error) {
	body, err := ResultBody(res)
	if err != nil {
		return nil, err
	}
	return jsonpath.Decode(body)
}

// ResultBody returns the response body, reading it from TempPath when the
// response was streamed to disk.
func ResultBody(res *ExecuteResult) ([]byte, error) {
	if res.TempPath == "" {
		return res.Body, nil
	}
	f, err := os.Open(res.TempPath)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxExtractBodySize))
}
