package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/store"
)

// fakeHistoryStore adds execution reads to fakeRunStore. runs are stored
// newest first, as the store returns them.
type fakeHistoryStore struct {
	fakeRunStore
	runs map[string][]*domain.Execution
}

func (s *fakeHistoryStore) ListExecutionsByRequest(
	_ context.Context,
	requestID string,
	limit int,
) ([]*domain.Execution, error) {
	runs := s.runs[requestID]
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	out := make([]*domain.Execution, 0, len(runs))
	for _, ex := range runs {
		list := *ex
		list.ResponseBody = ""
		list.BodyOmitted = true
		out = append(out, &list)
	}
	return out, nil
}

func (s *fakeHistoryStore) GetExecution(_ context.Context, id string) (*domain.Execution, error) {
	for _, runs := range s.runs {
		for _, ex := range runs {
			if ex.ID == id {
				return ex, nil
			}
		}
	}
	return nil, store.ErrNotFound
}

func newHistoryFixture() *fakeHistoryStore {
	completed := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	return &fakeHistoryStore{
		fakeRunStore: fakeRunStore{
			collections: []*domain.Collection{{ID: "c1", Name: "Payments"}},
			requests: map[string][]*domain.Request{
				"c1": {{ID: "r1", CollectionID: "c1", Method: "GET", Name: "List payments"}},
			},
		},
		runs: map[string][]*domain.Execution{
			"r1": {
				{
					ID:              "run-2b7f",
					RequestID:       "r1",
					StatusCode:      201,
					ResponseHeaders: `{"Content-Type":["application/json"],"X-Trace":["abc"]}`,
					ResponseBody:    `{"id":7,"ok":true}`,
					ResponseSize:    18,
					ResponseTimeMs:  42,
					CompletedAt:     completed,
					StartedAt:       completed,
				},
				{
					ID:              "run-9a10",
					RequestID:       "r1",
					StatusCode:      0,
					ResponseHeaders: "{}",
					ResponseBody:    "",
					ResponseTimeMs:  5,
					CompletedAt:     completed.Add(-time.Hour),
					StartedAt:       completed.Add(-time.Hour),
					Error:           "dial tcp: lookup\nexample.invalid: no such host",
				},
			},
		},
	}
}

func runHistory(t *testing.T, st HistoryStore, args ...string) (string, error) {
	t.Helper()
	cmd := NewHistoryCmd(st)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), err
}

func TestHistoryList_PrintsRunsNewestFirst(t *testing.T) {
	out, err := runHistory(t, newHistoryFixture(), "Payments/List payments")
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace([]byte(out)), []byte("\n"))
	require.Len(t, lines, 3, out)
	assert.Contains(t, string(lines[0]), "ID")
	assert.Contains(t, string(lines[1]), "run-2b7f")
	assert.Contains(t, string(lines[1]), "201")
	assert.Contains(t, string(lines[1]), "42ms")
	assert.Contains(t, string(lines[1]), "18 bytes")
	assert.Contains(t, string(lines[2]), "run-9a10")
	assert.Contains(t, string(lines[2]), "-")
	assert.Contains(t, string(lines[2]), "dial tcp: lookup example.invalid: no such host",
		"multi-line errors are flattened onto one row")
}

func TestHistoryList_LimitFlag(t *testing.T) {
	out, err := runHistory(t, newHistoryFixture(), "Payments/List payments", "--limit", "1")
	require.NoError(t, err)
	assert.Contains(t, out, "run-2b7f")
	assert.NotContains(t, out, "run-9a10")
}

func TestHistoryList_EmptyAndUnknownRequests(t *testing.T) {
	st := newHistoryFixture()
	st.runs = nil
	out, err := runHistory(t, st, "Payments/List payments")
	require.NoError(t, err)
	assert.Equal(t, "No history found.\n", out)

	_, err = runHistory(t, newHistoryFixture(), "Missing/List payments")
	require.ErrorContains(t, err, `collection "Missing" not found`)

	_, err = runHistory(t, newHistoryFixture(), "Payments/Nope")
	require.ErrorContains(t, err, `request "Nope" not found`)
}

func TestHistoryShow_PrintsOneRunWithHeadersAndBody(t *testing.T) {
	out, err := runHistory(t, newHistoryFixture(), "Payments/List payments", "run-2b7f")
	require.NoError(t, err)
	assert.Contains(t, out, "Run:       run-2b7f")
	assert.Contains(t, out, "Status:    201")
	assert.Contains(t, out, "Time:      42ms")
	assert.Contains(t, out, "Headers:\n  Content-Type: application/json\n  X-Trace: abc\n")
	assert.Contains(t, out, `"ok": true`, "a JSON body is pretty printed")
}

func TestHistoryShow_AcceptsUniquePrefix(t *testing.T) {
	out, err := runHistory(t, newHistoryFixture(), "Payments/List payments", "run-9")
	require.NoError(t, err)
	assert.Contains(t, out, "Run:       run-9a10")
	assert.Contains(t, out, "Error:     dial tcp: lookup example.invalid: no such host")
}

func TestHistoryShow_RejectsBadIDs(t *testing.T) {
	st := newHistoryFixture()
	_, err := runHistory(t, st, "Payments/List payments", "run-")
	require.ErrorContains(t, err, "ambiguous")
	_, err = runHistory(t, st, "Payments/List payments", "nope")
	require.ErrorContains(t, err, `execution "nope" not found`)
	_, err = runHistory(t, st, "Payments/List payments", "")
	require.ErrorContains(t, err, "cannot be empty")
}

func TestHistoryShow_NotesTruncatedBodies(t *testing.T) {
	st := newHistoryFixture()
	st.runs["r1"][0].ResponseBody = "partial"
	st.runs["r1"][0].ResponseBodyTruncated = true
	st.runs["r1"][0].ResponseSize = 4096
	out, err := runHistory(t, st, "Payments/List payments", "run-2b7f")
	require.NoError(t, err)
	assert.Contains(t, out, "Body:      stored first 7 of 4096 bytes")
	assert.Contains(t, out, "partial")
}

func TestStoredHeaders_MalformedJSONIsEmpty(t *testing.T) {
	assert.Empty(t, storedHeaders("not json"))
	assert.Empty(t, storedHeaders(""))
	assert.Equal(t, "abc", storedHeaders(`{"X-A":["abc"]}`).Get("X-A"))
}

func TestMatchExecution_AmbiguousAndMissing(t *testing.T) {
	runs := []*domain.Execution{{ID: "ab1"}, {ID: "ab2"}}
	_, err := matchExecution(runs, "ab")
	require.ErrorContains(t, err, `prefix "ab" is ambiguous (2 matches)`)
	got, err := matchExecution(runs, "ab2")
	require.NoError(t, err)
	assert.Equal(t, "ab2", got.ID)
}
