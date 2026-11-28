package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ExecutionWriter persists immutable execution history entries.
type ExecutionWriter interface {
	SaveExecution(ctx context.Context, ex *domain.Execution) error
}

type requestSnapshot struct {
	Method     string `json:"method"`
	URL        string `json:"url"`
	Headers    string `json:"headers"`
	AuthType   string `json:"auth_type,omitempty"`
	AuthConfig string `json:"auth_config,omitempty"`
	Body       string `json:"body"`
}

func (e *Executor) recordExecution(
	ctx context.Context,
	req *domain.Request,
	result *ExecuteResult,
	execErr error,
	startedAt, completedAt time.Time,
) {
	if e.executionWriter == nil || req == nil || req.ID == "" {
		return
	}

	ex, err := buildExecution(req, result, execErr, startedAt, completedAt, e.historyBodyLimit)
	if err != nil {
		e.logger.Warn("exec: build execution history entry failed", "err", err)
		return
	}

	saveCtx := context.WithoutCancel(ctx)
	if err := e.executionWriter.SaveExecution(saveCtx, ex); err != nil {
		e.logger.Warn("exec: save execution history failed", "err", err, "request_id", req.ID)
	}
}

func buildExecution(
	req *domain.Request,
	result *ExecuteResult,
	execErr error,
	startedAt, completedAt time.Time,
	bodyLimit int,
) (*domain.Execution, error) {
	snapshotBytes, err := json.Marshal(requestSnapshot{
		Method:     req.Method,
		URL:        req.URL,
		Headers:    req.Headers,
		AuthType:   req.AuthType,
		AuthConfig: req.AuthConfig,
		Body:       req.Body,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request snapshot: %w", err)
	}

	ex := &domain.Execution{
		ID:              uuid.NewString(),
		RequestID:       req.ID,
		RequestSnapshot: string(snapshotBytes),
		StartedAt:       startedAt.UTC(),
		CompletedAt:     completedAt.UTC(),
		ResponseHeaders: "{}",
	}

	if result != nil {
		headersBytes, err := json.Marshal(result.Headers)
		if err != nil {
			return nil, fmt.Errorf("marshal response headers: %w", err)
		}
		ex.StatusCode = result.StatusCode
		ex.ResponseHeaders = string(headersBytes)
		ex.ResponseTimeMs = result.Duration.Milliseconds()
		body, truncated, err := executionBody(result, bodyLimit)
		if err != nil {
			return nil, err
		}
		ex.ResponseBody = body
		ex.ResponseBodyTruncated = truncated
		ex.ResponseSize = result.Size
		if ex.ResponseSize < int64(len(body)) {
			ex.ResponseSize = int64(len(body))
		}
	} else {
		ex.ResponseTimeMs = completedAt.Sub(startedAt).Milliseconds()
	}

	if execErr != nil {
		ex.Error = execErr.Error()
	}

	return ex, nil
}

// executionBody returns at most limit bytes of the response body and whether
// the body was cut. A streamed body is read from its temp file only up to the
// limit. A limit of zero or less stores the whole body.
func executionBody(result *ExecuteResult, limit int) (string, bool, error) {
	if result == nil {
		return "", false, nil
	}
	if result.Body != nil {
		if limit > 0 && len(result.Body) > limit {
			return string(cutAtRuneBoundary(result.Body[:limit])), true, nil
		}
		return string(result.Body), false, nil
	}
	if result.TempPath == "" {
		return "", false, nil
	}
	f, err := os.Open(result.TempPath)
	if err != nil {
		return "", false, fmt.Errorf("read streamed response body: %w", err)
	}
	defer f.Close()
	var r io.Reader = f
	if limit > 0 {
		// Read one extra byte to learn whether the file is longer than limit.
		r = io.LimitReader(f, int64(limit)+1)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return "", false, fmt.Errorf("read streamed response body: %w", err)
	}
	if limit > 0 && len(body) > limit {
		return string(cutAtRuneBoundary(body[:limit])), true, nil
	}
	return string(body), false, nil
}

// cutAtRuneBoundary drops a trailing partial UTF-8 sequence that a byte cut
// left behind, so a truncated text body stays valid text.
func cutAtRuneBoundary(b []byte) []byte {
	for i := 0; i < utf8.UTFMax && i < len(b); i++ {
		r, size := utf8.DecodeLastRune(b[:len(b)-i])
		if r != utf8.RuneError || size > 1 {
			return b[:len(b)-i]
		}
	}
	return b
}
