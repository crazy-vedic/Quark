package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MultipartPart is one part of a multipart/form-data body. A part with File
// set sends the contents of that file, read when the request is sent; any
// other part sends Value as text.
type MultipartPart struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	// File is the path of the file to send.
	File string `json:"file,omitempty"`
	// Filename is sent in the part's Content-Disposition. It defaults to the
	// base name of File.
	Filename string `json:"filename,omitempty"`
	// ContentType is sent for a file part. It defaults to application/octet-stream.
	ContentType string `json:"content_type,omitempty"`
}

// ParseMultipartParts decodes Request.Body for the multipart body type. A
// blank body has no parts.
func ParseMultipartParts(body string) ([]MultipartPart, error) {
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	var parts []MultipartPart
	if err := json.Unmarshal([]byte(body), &parts); err != nil {
		return nil, fmt.Errorf("multipart body is not a JSON list of parts: %w", err)
	}
	return parts, nil
}

// MarshalMultipartParts encodes parts for Request.Body. No parts encode as "".
func MarshalMultipartParts(parts []MultipartPart) (string, error) {
	if len(parts) == 0 {
		return "", nil
	}
	data, err := json.Marshal(parts)
	if err != nil {
		return "", fmt.Errorf("encode multipart body: %w", err)
	}
	return string(data), nil
}
