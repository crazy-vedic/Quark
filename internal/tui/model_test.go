package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
)

// historyReader serves history entries without bodies from the list and with
// bodies from GetExecution, like the SQLite store.
type historyReader struct {
	entries []*domain.Execution
	listErr error
	getErr  error
	limit   int
	gets    []string
}

func (r *historyReader) ListExecutionsByRequest(
	_ context.Context, requestID string, limit int,
) ([]*domain.Execution, error) {
	r.limit = limit
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*domain.Execution
	for _, ex := range r.entries {
		if ex.RequestID == requestID {
			cp := *ex
			cp.ResponseBody = ""
			cp.BodyOmitted = true
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *historyReader) GetExecution(_ context.Context, id string) (*domain.Execution, error) {
	r.gets = append(r.gets, id)
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, ex := range r.entries {
		if ex.ID == id {
			cp := *ex
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}

func historyTestEntries() []*domain.Execution {
	now := time.Now()
	return []*domain.Execution{
		{
			ID:           "new",
			RequestID:    "req-1",
			StatusCode:   200,
			ResponseBody: "newest body",
			CompletedAt:  now,
		},
		{
			ID: "old", RequestID: "req-1", StatusCode: 500, ResponseBody: "older body",
			CompletedAt: now.Add(-time.Minute),
		},
	}
}

func historyModel(reader *historyReader) Model {
	m := New(Deps{Config: config.Default(""), ExecutionReader: reader})
	m.activeRequest = &domain.Request{ID: "req-1"}
	return m
}

func TestLoadExecutionHistoryCmd_LoadsOnlyNewestBody(t *testing.T) {
	reader := &historyReader{entries: historyTestEntries()}
	msg := loadExecutionHistoryCmd(context.Background(), reader, "req-1")()

	loaded, ok := msg.(executionHistoryLoadedMsg)
	require.True(t, ok)
	require.Len(t, loaded.executions, 2)
	assert.Equal(t, historyListLimit, reader.limit)
	assert.Equal(t, []string{"new"}, reader.gets)
	assert.False(t, loaded.executions[0].BodyOmitted)
	assert.Equal(t, "newest body", loaded.executions[0].ResponseBody)
	assert.True(t, loaded.executions[1].BodyOmitted)
}

func TestLoadExecutionHistoryCmd_BodyErrorIsReported(t *testing.T) {
	reader := &historyReader{entries: historyTestEntries(), getErr: errors.New("boom")}
	msg := loadExecutionHistoryCmd(context.Background(), reader, "req-1")()
	require.IsType(t, errLoadMsg{}, msg)
}

func TestHistoryNavigation_LoadsSelectedBodyOnDemand(t *testing.T) {
	reader := &historyReader{entries: historyTestEntries()}
	m := historyModel(reader)
	msg := loadExecutionHistoryCmd(context.Background(), reader, "req-1")()
	updated, _ := m.Update(msg)
	m = asModel(t, updated)

	updated, cmd := m.handleResponseAction("history_next")
	m = asModel(t, updated)
	require.Equal(t, 1, m.execCursor)
	require.True(t, m.selectedExecution().BodyOmitted)
	assert.Contains(t, m.viewExecutionBody(m.selectedExecution()), "Loading body")
	assert.Contains(t, m.viewExecutionRawBody(m.selectedExecution()), "Loading body")
	require.NotNil(t, cmd, "an omitted body must be fetched")

	updated, _ = m.Update(cmd())
	m = asModel(t, updated)
	assert.False(t, m.selectedExecution().BodyOmitted)
	assert.Equal(t, "older body", m.selectedExecution().ResponseBody)

	// Moving back to an entry whose body is loaded fetches nothing.
	updated, cmd = m.handleResponseAction("history_prev")
	m = asModel(t, updated)
	assert.Equal(t, 0, m.execCursor)
	assert.Nil(t, cmd)
}

func TestExecutionBodyLoadedMsg_IgnoredForOtherRequest(t *testing.T) {
	reader := &historyReader{entries: historyTestEntries()}
	m := historyModel(reader)
	m.executions = []*domain.Execution{{ID: "old", RequestID: "req-1", BodyOmitted: true}}

	updated, _ := m.Update(executionBodyLoadedMsg{
		requestID: "req-2",
		execution: &domain.Execution{ID: "old", ResponseBody: "x"},
	})
	assert.True(t, asModel(t, updated).executions[0].BodyOmitted)
}

func TestLoadExecutionBodyCmd_Error(t *testing.T) {
	reader := &historyReader{getErr: errors.New("gone")}
	msg := loadExecutionBodyCmd(context.Background(), reader, "req-1", "x")()
	require.IsType(t, errLoadMsg{}, msg)
}

func TestHistoryClick_LoadsSelectedBody(t *testing.T) {
	reader := &historyReader{entries: historyTestEntries()}
	m := historyModel(reader)
	m.executions = []*domain.Execution{
		historyTestEntries()[0],
		{ID: "old", RequestID: "req-1", BodyOmitted: true},
	}
	m.execCursor = 1
	cmd := m.loadSelectedExecutionBodyCmd()
	require.NotNil(t, cmd)
	loaded, ok := cmd().(executionBodyLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, "older body", loaded.execution.ResponseBody)

	m.executionReader = nil
	assert.Nil(t, m.loadSelectedExecutionBodyCmd(), "no reader, nothing to load")
}

func TestViewExecution_TruncatedBodyShowsNoteAndFullSize(t *testing.T) {
	m := New(Deps{Config: config.Default("")})
	ex := &domain.Execution{
		ID:                    "big",
		StatusCode:            200,
		ResponseBody:          "prefix",
		ResponseSize:          5000,
		ResponseBodyTruncated: true,
	}
	assert.Contains(t, m.viewExecutionBody(ex), "first 6 of 5000 bytes")
	assert.Contains(t, m.viewExecutionBody(ex), "prefix")
	assert.Contains(t, m.viewExecutionRawBody(ex), "first 6 of 5000 bytes")
	status := m.viewExecutionStatus(ex)
	assert.Contains(t, status, "5000 bytes")
	assert.Contains(t, status, "truncated")

	ex.ResponseBodyTruncated = false
	ex.ResponseSize = 0
	assert.Equal(t, "prefix", strings.TrimSpace(m.viewExecutionRawBody(ex)))
	assert.Contains(t, m.viewExecutionStatus(ex), "6 bytes")
	assert.Contains(t, m.viewExecutionRawBody(&domain.Execution{}), "empty body")
}

func asModel(t *testing.T, updated tea.Model) Model {
	t.Helper()
	m, ok := updated.(Model)
	require.True(t, ok)
	return m
}
