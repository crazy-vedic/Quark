package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

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

	ex, err := buildExecution(req, result, execErr, startedAt, completedAt)
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
		body, err := executionBody(result)
		if err != nil {
			return nil, err
		}
		ex.ResponseBody = body
	} else {
		ex.ResponseTimeMs = completedAt.Sub(startedAt).Milliseconds()
	}

	if execErr != nil {
		ex.Error = execErr.Error()
	}

	return ex, nil
}

func executionBody(result *ExecuteResult) (string, error) {
	if result == nil {
		return "", nil
	}
	if result.Body != nil {
		return string(result.Body), nil
	}
	if result.TempPath == "" {
		return "", nil
	}
	body, err := os.ReadFile(result.TempPath)
	if err != nil {
		return "", fmt.Errorf("read streamed response body: %w", err)
	}
	return string(body), nil
}
