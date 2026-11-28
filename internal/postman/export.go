package postman

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
)

// paramTypeString is the Postman type of every auth parameter Quark writes.
const paramTypeString = "string"

// SchemaV21 is the Postman Collection v2.1 schema URL written to exports.
const SchemaV21 = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// ExportFolder is a folder of Quark requests to export. The root folder
// holds the collection's own requests and sub-folders.
type ExportFolder struct {
	Name     string
	Requests []*domain.Request
	Folders  []*ExportFolder
}

// The types below mirror the Postman v2.1 shape with the omitempty rules the
// format needs. The importer's types are not reused: they would write an
// empty body or auth object for every request.

type exportDoc struct {
	Info     exportInfo   `json:"info"`
	Item     []exportItem `json:"item"`
	Variable []exportVar  `json:"variable,omitempty"`
}

type exportInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schema      string `json:"schema"`
}

type exportItem struct {
	Name    string         `json:"name"`
	Item    []exportItem   `json:"item,omitempty"`
	Request *exportRequest `json:"request,omitempty"`
}

type exportRequest struct {
	Method string         `json:"method"`
	Header []exportHeader `json:"header,omitempty"`
	URL    exportURL      `json:"url"`
	Body   *exportBody    `json:"body,omitempty"`
	Auth   *exportAuth    `json:"auth,omitempty"`
}

type exportURL struct {
	Raw string `json:"raw"`
}

type exportHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type exportBody struct {
	Mode    string             `json:"mode"`
	Raw     string             `json:"raw"`
	Options *exportBodyOptions `json:"options,omitempty"`
}

type exportBodyOptions struct {
	Raw exportRawOptions `json:"raw"`
}

type exportRawOptions struct {
	Language string `json:"language"`
}

type exportAuth struct {
	Type   string        `json:"type"`
	Bearer []exportParam `json:"bearer,omitempty"`
	Basic  []exportParam `json:"basic,omitempty"`
	APIKey []exportParam `json:"apikey,omitempty"`
}

type exportParam struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

type exportVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ExportCollection writes root as a Postman Collection v2.1 document. vars
// become the collection's variables, so {{names}} in URLs and bodies resolve
// after import. Folders without requests are left out. Output is
// deterministic: keys are sorted and no IDs or timestamps are written.
func ExportCollection(
	name, description string,
	root *ExportFolder,
	vars map[string]string,
) ([]byte, error) {
	items, err := exportItems(root)
	if err != nil {
		return nil, err
	}
	doc := exportDoc{
		Info: exportInfo{
			Name:        name,
			Description: description,
			Schema:      SchemaV21,
		},
		Item: items,
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		doc.Variable = append(doc.Variable, exportVar{Key: k, Value: vars[k]})
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("postman: encode collection: %w", err)
	}
	return append(data, '\n'), nil
}

func exportItems(f *ExportFolder) ([]exportItem, error) {
	items := []exportItem{}
	for _, req := range f.Requests {
		r, err := exportRequestOf(req)
		if err != nil {
			return nil, err
		}
		items = append(items, exportItem{Name: req.Name, Request: r})
	}
	for _, child := range f.Folders {
		if !hasRequests(child) {
			continue
		}
		sub, err := exportItems(child)
		if err != nil {
			return nil, err
		}
		items = append(items, exportItem{Name: child.Name, Item: sub})
	}
	return items, nil
}

func hasRequests(f *ExportFolder) bool {
	if len(f.Requests) > 0 {
		return true
	}
	for _, child := range f.Folders {
		if hasRequests(child) {
			return true
		}
	}
	return false
}

func exportRequestOf(req *domain.Request) (*exportRequest, error) {
	headers, err := domain.ParseHeaders(req.Headers)
	if err != nil {
		return nil, fmt.Errorf("request %q: headers: %w", req.Name, err)
	}
	out := &exportRequest{
		Method: req.Method,
		URL:    exportURL{Raw: req.URL},
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, v := range headers[name] {
			out.Header = append(out.Header, exportHeader{Key: name, Value: v})
		}
	}
	if req.Body != "" {
		out.Body = &exportBody{Mode: "raw", Raw: req.Body}
		if lang := languageFor(contentType(headers)); lang != "" {
			out.Body.Options = &exportBodyOptions{Raw: exportRawOptions{Language: lang}}
		}
	}
	out.Auth, err = exportAuthOf(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", req.Name, err)
	}
	return out, nil
}

// exportAuthOf maps a request's auth to the Postman auth block. Quark types
// without a Postman equivalent are an error, so nothing is silently dropped.
func exportAuthOf(req *domain.Request) (*exportAuth, error) {
	authType := domain.NormalizeAuthType(req.AuthType)
	if authType == domain.AuthTypeNone {
		return nil, nil
	}
	cfg, err := domain.ParseAuthConfig(req.AuthConfig)
	if err != nil {
		return nil, fmt.Errorf("auth config: %w", err)
	}
	switch authType {
	case domain.AuthTypeBearer:
		return &exportAuth{
			Type:   "bearer",
			Bearer: []exportParam{{Key: "token", Value: cfg.Token, Type: paramTypeString}},
		}, nil
	case domain.AuthTypeBasic:
		return &exportAuth{
			Type: "basic",
			Basic: []exportParam{
				{Key: "username", Value: cfg.Username, Type: paramTypeString},
				{Key: "password", Value: cfg.Password, Type: paramTypeString},
			},
		}, nil
	case domain.AuthTypeAPIKey:
		return &exportAuth{
			Type: "apikey",
			APIKey: []exportParam{
				{Key: "key", Value: cfg.Name, Type: paramTypeString},
				{Key: "value", Value: cfg.Value, Type: paramTypeString},
				{Key: "in", Value: domain.NormalizeAPIKeyIn(cfg.In), Type: paramTypeString},
			},
		}, nil
	}
	return nil, fmt.Errorf("auth type %q has no Postman equivalent", req.AuthType)
}

// contentType returns the first Content-Type value, matched case-insensitively.
func contentType(headers map[string][]string) string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.EqualFold(name, "Content-Type") && len(headers[name]) > 0 {
			return headers[name][0]
		}
	}
	return ""
}

// languageFor returns the Postman body language for a Content-Type, or "".
func languageFor(contentType string) string {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch {
	case base == "application/json" || strings.HasSuffix(base, "+json"):
		return "json"
	case base == "application/xml" || base == "text/xml" || strings.HasSuffix(base, "+xml"):
		return "xml"
	case base == "text/html":
		return "html"
	case base == "text/plain":
		return "text"
	case base == "application/javascript":
		return "javascript"
	}
	return ""
}
