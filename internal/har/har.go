// Package har converts Quark execution history into HTTP Archive (HAR) 1.2
// documents, the format browsers and most HTTP tools can open.
package har

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/crazy-vedic/quark/internal/domain"
)

// Version is the HAR format version written to every log.
const Version = "1.2"

// Encode returns log as an indented HAR document, which wraps it in a
// top-level "log" object as the format requires.
func Encode(log *Log) ([]byte, error) {
	data, err := json.MarshalIndent(struct {
		Log *Log `json:"log"`
	}{Log: log}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("har: encode: %w", err)
	}
	return append(data, '\n'), nil
}

// Log is the content of a HAR document (its "log" object).
type Log struct {
	Version string  `json:"version"`
	Creator Creator `json:"creator"`
	Entries []Entry `json:"entries"`
}

// Creator names the tool that wrote the log.
type Creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Entry is one request and its response.
type Entry struct {
	StartedDateTime string   `json:"startedDateTime"`
	Time            float64  `json:"time"`
	Request         Request  `json:"request"`
	Response        Response `json:"response"`
	Cache           struct{} `json:"cache"`
	Timings         Timings  `json:"timings"`
	Comment         string   `json:"comment,omitempty"`
}

// Request is the HAR request part of an entry.
type Request struct {
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	HTTPVersion string      `json:"httpVersion"`
	Cookies     []NameValue `json:"cookies"`
	Headers     []NameValue `json:"headers"`
	QueryString []NameValue `json:"queryString"`
	PostData    *PostData   `json:"postData,omitempty"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

// Response is the HAR response part of an entry.
type Response struct {
	Status      int         `json:"status"`
	StatusText  string      `json:"statusText"`
	HTTPVersion string      `json:"httpVersion"`
	Cookies     []NameValue `json:"cookies"`
	Headers     []NameValue `json:"headers"`
	Content     Content     `json:"content"`
	RedirectURL string      `json:"redirectURL"`
	HeadersSize int         `json:"headersSize"`
	BodySize    int         `json:"bodySize"`
}

// NameValue is a header, cookie or query parameter.
type NameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// PostData is the request body.
type PostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// Content is the response body.
type Content struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// Timings splits the entry's time into phases, in milliseconds. Quark does
// not measure the send and receive phases, so they are 0.
type Timings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

// snapshot mirrors the JSON that the executor stores for each execution.
type snapshot struct {
	Method     string `json:"method"`
	URL        string `json:"url"`
	Headers    string `json:"headers"`
	AuthType   string `json:"auth_type,omitempty"`
	AuthConfig string `json:"auth_config,omitempty"`
	Body       string `json:"body"`
}

// FromExecutions builds a HAR log from execution history. Entries are
// ordered oldest first. creatorVersion is the Quark version written to the
// log's creator field.
func FromExecutions(execs []*domain.Execution, creatorVersion string) (*Log, error) {
	log := &Log{
		Version: Version,
		Creator: Creator{Name: "Quark", Version: creatorVersion},
		Entries: []Entry{},
	}
	sorted := append([]*domain.Execution(nil), execs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].StartedAt.Before(sorted[j].StartedAt)
	})
	for _, ex := range sorted {
		entry, err := entryOf(ex)
		if err != nil {
			return nil, fmt.Errorf("har: execution %q: %w", ex.ID, err)
		}
		log.Entries = append(log.Entries, entry)
	}
	return log, nil
}

func entryOf(ex *domain.Execution) (Entry, error) {
	var snap snapshot
	if err := json.Unmarshal([]byte(ex.RequestSnapshot), &snap); err != nil {
		return Entry{}, fmt.Errorf("request snapshot: %w", err)
	}
	reqHeaders, err := domain.ParseHeaders(snap.Headers)
	if err != nil {
		return Entry{}, fmt.Errorf("request headers: %w", err)
	}
	respHeaders, err := domain.ParseHeaders(ex.ResponseHeaders)
	if err != nil {
		return Entry{}, fmt.Errorf("response headers: %w", err)
	}
	query, err := queryOf(snap.URL)
	if err != nil {
		return Entry{}, err
	}
	if err := addAuth(snap, reqHeaders, &query); err != nil {
		return Entry{}, err
	}

	contentType := headerValue(respHeaders, "Content-Type")
	reqType := headerValue(reqHeaders, "Content-Type")
	entry := Entry{
		StartedDateTime: ex.StartedAt.UTC().Format(time.RFC3339Nano),
		Time:            float64(ex.ResponseTimeMs),
		Timings: Timings{
			Send:    0,
			Wait:    float64(ex.ResponseTimeMs),
			Receive: 0,
		},
		Comment: ex.Error,
		Request: Request{
			Method:      snap.Method,
			URL:         snap.URL,
			HTTPVersion: "HTTP/1.1",
			Cookies:     []NameValue{},
			Headers:     sortedNameValues(reqHeaders),
			QueryString: query,
			HeadersSize: -1,
			BodySize:    len(snap.Body),
		},
		Response: Response{
			Status:      ex.StatusCode,
			StatusText:  statusText(ex.StatusCode),
			HTTPVersion: "HTTP/1.1",
			Cookies:     []NameValue{},
			Headers:     sortedNameValues(respHeaders),
			Content: Content{
				Size:     len(ex.ResponseBody),
				MimeType: contentType,
				Text:     ex.ResponseBody,
			},
			RedirectURL: "",
			HeadersSize: -1,
			BodySize:    len(ex.ResponseBody),
		},
	}
	if snap.Body != "" {
		entry.Request.PostData = &PostData{MimeType: reqType, Text: snap.Body}
	}
	return entry, nil
}

// addAuth adds the credentials that were sent with the request, so the
// archive shows the same headers and query as the wire. Basic and bearer
// credentials go into Authorization; API keys go into a header or the query.
func addAuth(snap snapshot, headers map[string][]string, query *[]NameValue) error {
	if snap.AuthType == "" || domain.NormalizeAuthType(snap.AuthType) == domain.AuthTypeNone {
		return nil
	}
	cfg, err := domain.ParseAuthConfig(snap.AuthConfig)
	if err != nil {
		return fmt.Errorf("auth config: %w", err)
	}
	switch domain.NormalizeAuthType(snap.AuthType) {
	case domain.AuthTypeBearer:
		headers["Authorization"] = []string{"Bearer " + cfg.Token}
	case domain.AuthTypeBasic:
		creds := cfg.Username + ":" + cfg.Password
		encoded := base64.StdEncoding.EncodeToString([]byte(creds))
		headers["Authorization"] = []string{"Basic " + encoded}
	case domain.AuthTypeAPIKey:
		if domain.NormalizeAPIKeyIn(cfg.In) == domain.AuthAPIKeyInQuery {
			*query = append(*query, NameValue{Name: cfg.Name, Value: cfg.Value})
		} else {
			headers[cfg.Name] = []string{cfg.Value}
		}
	}
	return nil
}

// queryOf returns the query parameters of rawURL in the order they appear.
// A URL that does not parse has no query.
func queryOf(rawURL string) ([]NameValue, error) {
	out := []NameValue{}
	u, err := url.Parse(rawURL)
	if err != nil || u.RawQuery == "" {
		return out, nil
	}
	for _, pair := range strings.Split(u.RawQuery, "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		name, errName := url.QueryUnescape(name)
		value, errValue := url.QueryUnescape(value)
		if errName != nil || errValue != nil {
			return nil, fmt.Errorf("query of %q: invalid escape", rawURL)
		}
		out = append(out, NameValue{Name: name, Value: value})
	}
	return out, nil
}

// headerValue returns the first value of a header, matched case-insensitively.
func headerValue(headers map[string][]string, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// sortedNameValues returns headers as name/value pairs sorted by name, with
// one pair per value.
func sortedNameValues(headers map[string][]string) []NameValue {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []NameValue{}
	for _, name := range names {
		for _, v := range headers[name] {
			out = append(out, NameValue{Name: name, Value: v})
		}
	}
	return out
}

func statusText(code int) string {
	if code == 0 {
		return ""
	}
	return http.StatusText(code)
}
