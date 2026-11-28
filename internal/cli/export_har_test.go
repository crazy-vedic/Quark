package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestExportHARWritesExecutionHistory(t *testing.T) {
	ctx := context.Background()
	st := newExportTestStore(t)
	seedExportCollection(t, st)
	req, err := st.ResolveRequestPath(ctx, "API/Create item")
	require.NoError(t, err)

	started := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	runs := []struct {
		status int
		body   string
	}{
		{500, `{"error":"boom"}`},
		{201, `{"id":7}`},
	}
	for i, run := range runs {
		require.NoError(t, st.SaveExecution(ctx, &domain.Execution{
			ID:        []string{"first", "second"}[i],
			RequestID: req.ID,
			RequestSnapshot: `{"method":"POST","url":"https://api.example.com/items",` +
				`"headers":"{\"Content-Type\":\"application/json\"}","auth_type":"bearer",` +
				`"auth_config":"{\"token\":\"tok\"}","body":"{\"name\":\"widget\"}"}`,
			StatusCode:      run.status,
			ResponseHeaders: `{"Content-Type":["application/json"]}`,
			ResponseBody:    run.body,
			ResponseTimeMs:  int64(10 + i),
			StartedAt:       started.Add(time.Duration(i) * time.Minute),
			CompletedAt:     started.Add(time.Duration(i)*time.Minute + time.Second),
		}))
	}

	var stderr bytes.Buffer
	cmd := NewExportHARCmd(st, "9.9.9")
	cmd.SetErr(&stderr)
	out := runExportCmd(t, cmd, "API/Create item")

	var doc struct {
		Log struct {
			Version string `json:"version"`
			Creator struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"creator"`
			Entries []struct {
				StartedDateTime string `json:"startedDateTime"`
				Request         struct {
					Method   string                         `json:"method"`
					URL      string                         `json:"url"`
					Headers  []struct{ Name, Value string } `json:"headers"`
					PostData struct {
						MimeType string `json:"mimeType"`
						Text     string `json:"text"`
					} `json:"postData"`
				} `json:"request"`
				Response struct {
					Status     int    `json:"status"`
					StatusText string `json:"statusText"`
				} `json:"response"`
				Timings struct {
					Wait float64 `json:"wait"`
				} `json:"timings"`
			} `json:"entries"`
		} `json:"log"`
	}
	require.NoError(t, json.Unmarshal(out, &doc), string(out))
	assert.Equal(t, "1.2", doc.Log.Version)
	assert.Equal(t, "Quark", doc.Log.Creator.Name)
	assert.Equal(t, "9.9.9", doc.Log.Creator.Version)
	require.Len(t, doc.Log.Entries, 2)

	first, second := doc.Log.Entries[0], doc.Log.Entries[1]
	assert.Equal(t, "2026-11-27T09:00:00Z", first.StartedDateTime, "oldest entry first")
	assert.Equal(t, 500, first.Response.Status)
	assert.Equal(t, "Internal Server Error", first.Response.StatusText)
	assert.Equal(t, 201, second.Response.Status)
	assert.Equal(t, "Created", second.Response.StatusText)
	assert.Equal(t, "POST", first.Request.Method)
	assert.Equal(t, "https://api.example.com/items", first.Request.URL)
	assert.Equal(t, `{"name":"widget"}`, first.Request.PostData.Text)
	assert.Equal(t, "application/json", first.Request.PostData.MimeType)
	assert.Equal(t, 10.0, first.Timings.Wait)
	assert.Contains(t, first.Request.Headers, struct{ Name, Value string }{
		Name: "Authorization", Value: "Bearer tok",
	}, "the bearer header that was sent is listed")
	assert.Empty(t, stderr.String())
}

func TestExportHARWithoutHistoryWarnsAndWritesEmptyLog(t *testing.T) {
	st := newExportTestStore(t)
	seedExportCollection(t, st)

	var stderr bytes.Buffer
	cmd := NewExportHARCmd(st, "dev")
	cmd.SetErr(&stderr)
	out := runExportCmd(t, cmd, "API/Users/Get user")

	var doc struct {
		Log struct {
			Entries []any `json:"entries"`
		} `json:"log"`
	}
	require.NoError(t, json.Unmarshal(out, &doc), string(out))
	assert.Empty(t, doc.Log.Entries)
	assert.Contains(t, stderr.String(), `No execution history for "API/Users/Get user"`)
}

func TestExportHARUnknownRequest(t *testing.T) {
	st := newExportTestStore(t)
	cmd := NewExportHARCmd(st, "dev")
	cmd.SetArgs([]string{"API/Missing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	require.ErrorContains(t, cmd.Execute(), "export-har: resolve request")
}
