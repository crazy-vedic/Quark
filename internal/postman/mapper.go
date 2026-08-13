package postman

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/url"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ImportCollection converts a Postman collection into Quark domain requests.
// It flattens nested folders into request names with "/" separators.
func ImportCollection(c *Collection) *ImportResult {
	result := &ImportResult{
		CollectionName: c.Info.Name,
	}

	if c.Info.Name == "" {
		result.CollectionName = "Imported"
	}

	var idx int
	var walk func(items []Item, prefix string)
	walk = func(items []Item, prefix string) {
		for _, item := range items {
			if item.Request != nil {
				req, warnings := mapRequest(item, prefix, idx)
				if req != nil {
					result.Requests = append(result.Requests, req)
					result.Warnings = append(result.Warnings, warnings...)
					idx++
				}
			} else if len(item.Item) > 0 {
				// Nested folder: recurse with updated prefix
				newPrefix := prefix
				if newPrefix != "" {
					newPrefix += "/"
				}
				newPrefix += item.Name
				walk(item.Item, newPrefix)
			}
		}
	}

	walk(c.Item, "")

	return result
}

func mapRequest(item Item, prefix string, sortOrder int) (*domain.Request, []string) {
	if item.Request == nil {
		return nil, nil
	}

	req := item.Request

	// Build name: prefix/ItemName or just ItemName
	name := item.Name
	if prefix != "" {
		name = prefix + "/" + name
	}

	// Extract URL
	requestURL := req.URL.Raw
	if requestURL == "" {
		// Try to reconstruct from host + path
		requestURL = buildURL(req.URL)
	}

	// Extract headers as JSON object
	headersMap := make(map[string]string)
	for _, h := range req.Header {
		if h.Key != "" && h.Value != "" {
			headersMap[h.Key] = h.Value.String()
		}
	}

	// Extract authentication as headers
	warnings := mapAuth(req.Auth, headersMap)

	// Body
	body := ""
	switch req.Body.Mode {
	case "raw":
		body = req.Body.Raw
		// Auto-set Content-Type from body language if not already present
		if _, ok := headersMap["Content-Type"]; !ok && req.Body.Options.Raw.Language != "" {
			ct := contentTypeFromLanguage(req.Body.Options.Raw.Language)
			if ct != "" {
				headersMap["Content-Type"] = ct
			}
		}
	case "urlencoded":
		body = encodeURLEncodedBody(req.Body.URLEncoded)
		if _, ok := headersMap["Content-Type"]; !ok {
			headersMap["Content-Type"] = "application/x-www-form-urlencoded"
		}
	case "formdata":
		var contentType string
		body, contentType = encodeFormDataBody(req.Body.FormData)
		if _, ok := headersMap["Content-Type"]; !ok {
			headersMap["Content-Type"] = contentType
		}
	case "", "none":
		// No body.
	default:
		// Unsupported body mode: mark with warning but still import without body
		warnings = append(
			warnings,
			fmt.Sprintf("unsupported body mode %q for request %q", req.Body.Mode, name),
		)
	}

	headersJSON := "{}"
	if len(headersMap) > 0 {
		b, _ := json.Marshal(headersMap)
		headersJSON = string(b)
	}

	return &domain.Request{
		ID:        uuid.New().String(),
		Name:      name,
		Method:    strings.ToUpper(req.Method),
		URL:       requestURL,
		Headers:   headersJSON,
		Body:      body,
		SortOrder: sortOrder,
	}, warnings
}

func encodeURLEncodedBody(params []BodyParam) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		if p.Disabled {
			continue
		}
		parts = append(
			parts,
			escapeFormComponent(p.Key)+"="+escapeFormComponent(p.Value.String()),
		)
	}
	return strings.Join(parts, "&")
}

func escapeFormComponent(s string) string {
	var out strings.Builder
	for {
		start := strings.Index(s, "{{")
		if start == -1 {
			out.WriteString(url.QueryEscape(s))
			return out.String()
		}
		endRel := strings.Index(s[start+2:], "}}")
		if endRel == -1 {
			out.WriteString(url.QueryEscape(s))
			return out.String()
		}
		end := start + 2 + endRel + 2
		out.WriteString(url.QueryEscape(s[:start]))
		out.WriteString(s[start:end])
		s = s[end:]
	}
}

func encodeFormDataBody(params []FormDataParam) (string, string) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.SetBoundary("quark-postman-boundary")

	for _, p := range params {
		if p.Disabled {
			continue
		}
		if p.Type == "file" {
			for _, src := range formDataSources(p.Src) {
				filename := path.Base(strings.ReplaceAll(src, `\`, `/`))
				if filename == "." || filename == "/" {
					filename = p.Key
				}
				part, err := writer.CreateFormFile(p.Key, filename)
				if err == nil {
					_, _ = part.Write(nil)
				}
			}
			continue
		}
		_ = writer.WriteField(p.Key, p.Value.String())
	}

	_ = writer.Close()
	return buf.String(), writer.FormDataContentType()
}

func formDataSources(raw json.RawMessage) []string {
	var src string
	if err := json.Unmarshal(raw, &src); err == nil && src != "" {
		return []string{src}
	}

	var srcs []string
	if err := json.Unmarshal(raw, &srcs); err == nil {
		out := make([]string, 0, len(srcs))
		for _, src := range srcs {
			if src != "" {
				out = append(out, src)
			}
		}
		return out
	}

	return nil
}

// buildURL reconstructs a URL from Postman's structured URL fields.
func buildURL(u URL) string {
	if u.Raw != "" {
		return u.Raw
	}

	var parts []string
	if len(u.Host) > 0 {
		parts = append(parts, strings.Join(u.Host, "."))
	}
	for _, p := range u.Path {
		switch v := p.(type) {
		case string:
			parts = append(parts, v)
		case map[string]any:
			if key, ok := v["key"].(string); ok {
				parts = append(parts, key)
			}
		}
	}

	scheme := u.Protocol
	if scheme == "" {
		scheme = "https"
	}
	path := strings.Join(parts, "/")
	if path == "" {
		path = "/"
	}

	// Check if URL already has a scheme
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	// Add query parameters
	if len(u.Query) > 0 {
		q := url.Values{}
		for _, qp := range u.Query {
			q.Add(qp.Key, qp.Value.String())
		}
		path += "?" + q.Encode()
	}

	return scheme + "://" + path
}

// mapAuth converts Postman authentication to Authorization headers.
// Returns warnings for unsupported auth types.
func mapAuth(auth *Auth, headers map[string]string) []string {
	if auth == nil {
		return nil
	}

	var warnings []string

	switch auth.Type {
	case "bearer":
		var token string
		for _, p := range auth.Bearer {
			if p.Key == "token" {
				token = p.Value.String()
			}
		}
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		} else {
			warnings = append(warnings, "bearer auth token is empty; Authorization header not set")
		}
	case "basic":
		var username, password string
		for _, p := range auth.Basic {
			if p.Key == "username" {
				username = p.Value.String()
			}
			if p.Key == "password" {
				password = p.Value.String()
			}
		}
		if username != "" {
			// Basic auth: base64-encode per RFC 7617, consistent with curl importer.
			creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
			headers["Authorization"] = "Basic " + creds
		}
	case "apikey":
		var keyName, value, in string
		for _, p := range auth.APIKey {
			switch p.Key {
			case "key":
				keyName = p.Value.String()
			case "value":
				value = p.Value.String()
			case "in":
				in = p.Value.String()
			}
		}
		switch {
		case keyName == "":
			warnings = append(warnings, "apikey auth missing key name; header not set")
		case in != "header":
			warnings = append(
				warnings,
				fmt.Sprintf("apikey auth location %q not supported; header import only", in),
			)
		default:
			headers[keyName] = value
		}
	case "noauth":
		// No auth, nothing to do
	case "":
		// Empty auth type
	default:
		warnings = append(warnings, fmt.Sprintf("unsupported auth type %q (skipped)", auth.Type))
	}

	return warnings
}

// contentTypeFromLanguage maps Postman body language identifiers to Content-Type headers.
func contentTypeFromLanguage(lang string) string {
	switch lang {
	case "json":
		return "application/json"
	case "xml":
		return "application/xml"
	case "text":
		return "text/plain"
	case "html":
		return "text/html"
	case "javascript":
		return "application/javascript"
	default:
		return ""
	}
}
