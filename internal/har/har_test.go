package har

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func snapshotJSON(t *testing.T, snap snapshot) string {
	t.Helper()
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	return string(data)
}

func TestFromExecutionsBuildsHAR12(t *testing.T) {
	started := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	execs := []*domain.Execution{
		{
			ID:        "later",
			StartedAt: started.Add(time.Minute),
			RequestSnapshot: snapshotJSON(t, snapshot{
				Method:  "GET",
				URL:     "https://api.example.com/items?limit=10&sort=name",
				Headers: `{"Accept":"application/json"}`,
				Body:    "",
			}),
			StatusCode:      500,
			ResponseHeaders: `{"Content-Type":["text/plain"]}`,
			ResponseBody:    "boom",
			ResponseTimeMs:  42,
		},
		{
			ID:        "first",
			StartedAt: started,
			RequestSnapshot: snapshotJSON(t, snapshot{
				Method:     "POST",
				URL:        "https://api.example.com/items",
				Headers:    `{"Content-Type":"application/json","X-Trace":"t-1"}`,
				AuthType:   domain.AuthTypeBearer,
				AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{Token: "tok"}),
				Body:       `{"name":"widget"}`,
			}),
			StatusCode:      201,
			ResponseHeaders: `{"Content-Type":["application/json"],"X-Id":["7"]}`,
			ResponseBody:    `{"id":7}`,
			ResponseTimeMs:  120,
		},
	}

	log, err := FromExecutions(execs, "1.2.3")
	require.NoError(t, err)
	assert.Equal(t, "1.2", log.Version)
	assert.Equal(t, "Quark", log.Creator.Name)
	assert.Equal(t, "1.2.3", log.Creator.Version)
	require.Len(t, log.Entries, 2)

	// Entries are oldest first.
	first, later := log.Entries[0], log.Entries[1]
	assert.Equal(t, "2026-11-27T10:00:00Z", first.StartedDateTime)
	assert.Equal(t, "POST", first.Request.Method)
	assert.Equal(t, "HTTP/1.1", first.Request.HTTPVersion)
	assert.Equal(t, []NameValue{
		{Name: "Authorization", Value: "Bearer tok"},
		{Name: "Content-Type", Value: "application/json"},
		{Name: "X-Trace", Value: "t-1"},
	}, first.Request.Headers, "sent headers include the auth header, sorted by name")
	require.NotNil(t, first.Request.PostData)
	assert.Equal(t, "application/json", first.Request.PostData.MimeType)
	assert.Equal(t, `{"name":"widget"}`, first.Request.PostData.Text)
	assert.Equal(t, len(`{"name":"widget"}`), first.Request.BodySize)
	assert.Equal(t, 201, first.Response.Status)
	assert.Equal(t, "Created", first.Response.StatusText)
	assert.Equal(t, `{"id":7}`, first.Response.Content.Text)
	assert.Equal(t, "application/json", first.Response.Content.MimeType)
	assert.Equal(t, []NameValue{
		{Name: "Content-Type", Value: "application/json"},
		{Name: "X-Id", Value: "7"},
	}, first.Response.Headers)
	assert.Equal(t, Timings{Send: 0, Wait: 120, Receive: 0}, first.Timings)
	assert.Equal(t, 120.0, first.Time)
	assert.Empty(t, first.Comment)

	assert.Nil(t, later.Request.PostData, "no body, no postData")
	assert.Equal(t, []NameValue{
		{Name: "limit", Value: "10"},
		{Name: "sort", Value: "name"},
	}, later.Request.QueryString, "query order is kept")
	assert.Equal(t, 500, later.Response.Status)
	assert.Equal(t, "Internal Server Error", later.Response.StatusText)
}

func TestFromExecutionsAddsAPIKeyToQueryOrHeader(t *testing.T) {
	ex := func(id string, in string) *domain.Execution {
		return &domain.Execution{
			ID: id,
			RequestSnapshot: snapshotJSON(t, snapshot{
				Method:   "GET",
				URL:      "https://api.example.com/items?page=2",
				Headers:  "{}",
				AuthType: domain.AuthTypeAPIKey,
				AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
					Name: "X-Key", Value: "secret", In: in,
				}),
			}),
			StatusCode:      200,
			ResponseHeaders: "{}",
		}
	}
	log, err := FromExecutions([]*domain.Execution{
		ex("h", domain.AuthAPIKeyInHeader),
		ex("q", domain.AuthAPIKeyInQuery),
	}, "dev")
	require.NoError(t, err)

	header, query := log.Entries[0], log.Entries[1]
	assert.Equal(t, []NameValue{{Name: "X-Key", Value: "secret"}}, header.Request.Headers)
	assert.Equal(t, []NameValue{{Name: "page", Value: "2"}}, header.Request.QueryString)
	assert.Empty(t, query.Request.Headers)
	assert.Equal(t, []NameValue{
		{Name: "page", Value: "2"},
		{Name: "X-Key", Value: "secret"},
	}, query.Request.QueryString)
}

func TestFromExecutionsBasicAuth(t *testing.T) {
	log, err := FromExecutions([]*domain.Execution{{
		ID: "b",
		RequestSnapshot: snapshotJSON(t, snapshot{
			Method: "GET", URL: "https://api.example.com/me", Headers: "{}",
			AuthType: domain.AuthTypeBasic,
			AuthConfig: domain.MustAuthConfigJSON(domain.AuthConfig{
				Username: "u", Password: "p",
			}),
		}),
		ResponseHeaders: "{}",
	}}, "dev")
	require.NoError(t, err)
	assert.Equal(t, []NameValue{{Name: "Authorization", Value: "Basic dTpw"}},
		log.Entries[0].Request.Headers)
}

func TestFromExecutionsFailedRequest(t *testing.T) {
	log, err := FromExecutions([]*domain.Execution{{
		ID: "e",
		RequestSnapshot: snapshotJSON(t, snapshot{
			Method: "GET", URL: "https://down.example.com/", Headers: "{}",
		}),
		ResponseHeaders: "{}",
		Error:           "dial tcp: connection refused",
	}}, "dev")
	require.NoError(t, err)
	entry := log.Entries[0]
	assert.Equal(t, 0, entry.Response.Status)
	assert.Empty(t, entry.Response.StatusText)
	assert.Equal(t, "dial tcp: connection refused", entry.Comment)
	assert.Empty(t, entry.Request.Headers)
	assert.Equal(t, []NameValue{}, entry.Request.QueryString, "empty lists are arrays, not null")
}

func TestEncodedDocumentHasHAR12Fields(t *testing.T) {
	log, err := FromExecutions(nil, "dev")
	require.NoError(t, err)
	data, err := Encode(log)
	require.NoError(t, err)

	var doc struct {
		Log struct {
			Version string           `json:"version"`
			Creator map[string]any   `json:"creator"`
			Entries []map[string]any `json:"entries"`
		} `json:"log"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, "1.2", doc.Log.Version)
	assert.Equal(t, "Quark", doc.Log.Creator["name"])
	assert.NotNil(t, doc.Log.Entries, "entries is an array even when empty")
	assert.Empty(t, doc.Log.Entries)
}

func TestMarshalledEntryHasRequiredKeys(t *testing.T) {
	log, err := FromExecutions([]*domain.Execution{{
		ID: "k",
		RequestSnapshot: snapshotJSON(t, snapshot{
			Method: "GET", URL: "https://api.example.com/", Headers: "{}",
		}),
		StatusCode:      200,
		ResponseHeaders: "{}",
		StartedAt:       time.Now(),
	}}, "dev")
	require.NoError(t, err)
	data, err := json.Marshal(log.Entries[0])
	require.NoError(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(data, &entry))
	for _, key := range []string{
		"startedDateTime", "time", "request", "response", "cache", "timings",
	} {
		assert.Contains(t, entry, key)
	}
	timings, ok := entry["timings"].(map[string]any)
	require.True(t, ok, "timings is an object")
	for _, key := range []string{"send", "wait", "receive"} {
		assert.Contains(t, timings, key)
	}
}

func TestFromExecutionsRejectsCorruptSnapshot(t *testing.T) {
	_, err := FromExecutions([]*domain.Execution{{
		ID: "bad", RequestSnapshot: "{not json", ResponseHeaders: "{}",
	}}, "dev")
	require.ErrorContains(t, err, `har: execution "bad"`)
}
