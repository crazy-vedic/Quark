package curl

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type optionKind uint8

const (
	optionHeader optionKind = iota
	optionMethod
	optionData
	optionDataRaw
	optionDataBinary
	optionDataURLEncode
	optionJSON
	optionForm
	optionFormString
	optionUser
	optionCookie
	optionURL
	optionURLQuery
	optionCert
	optionCertType
	optionKey
	optionCACert
	optionHead
	optionGet
	optionUserAgent
	optionReferer
	optionIgnore
)

type optionDefinition struct {
	kind     optionKind
	hasValue bool
	warning  string
}

var supportedOptions = map[string]optionDefinition{
	"-H":               {kind: optionHeader, hasValue: true},
	"--header":         {kind: optionHeader, hasValue: true},
	"-X":               {kind: optionMethod, hasValue: true},
	"--request":        {kind: optionMethod, hasValue: true},
	"-d":               {kind: optionData, hasValue: true},
	"--data":           {kind: optionData, hasValue: true},
	"--data-ascii":     {kind: optionData, hasValue: true},
	"--data-raw":       {kind: optionDataRaw, hasValue: true},
	"--data-binary":    {kind: optionDataBinary, hasValue: true},
	"--data-urlencode": {kind: optionDataURLEncode, hasValue: true},
	"--json":           {kind: optionJSON, hasValue: true},
	"-F":               {kind: optionForm, hasValue: true},
	"--form":           {kind: optionForm, hasValue: true},
	"--form-string":    {kind: optionFormString, hasValue: true},
	"-u":               {kind: optionUser, hasValue: true},
	"--user":           {kind: optionUser, hasValue: true},
	"-b":               {kind: optionCookie, hasValue: true},
	"--cookie":         {kind: optionCookie, hasValue: true},
	"--url":            {kind: optionURL, hasValue: true},
	"--url-query":      {kind: optionURLQuery, hasValue: true},
	"-E":               {kind: optionCert, hasValue: true},
	"--cert":           {kind: optionCert, hasValue: true},
	"--cert-type":      {kind: optionCertType, hasValue: true},
	"--key":            {kind: optionKey, hasValue: true},
	"--cacert":         {kind: optionCACert, hasValue: true},
	"-I":               {kind: optionHead},
	"--head":           {kind: optionHead},
	"-G":               {kind: optionGet},
	"--get":            {kind: optionGet},
	"-A":               {kind: optionUserAgent, hasValue: true},
	"--user-agent":     {kind: optionUserAgent, hasValue: true},
	"-e":               {kind: optionReferer, hasValue: true},
	"--referer":        {kind: optionReferer, hasValue: true},

	"-L":                 {kind: optionIgnore, warning: "redirect behavior is not imported; Quark's configured redirect policy applies"},
	"--location":         {kind: optionIgnore, warning: "redirect behavior is not imported; Quark's configured redirect policy applies"},
	"--location-trusted": {kind: optionIgnore, warning: "trusted redirect behavior is not imported"},
	"--compressed":       {kind: optionIgnore, warning: "compression behavior is delegated to Quark's HTTP transport"},
	"-f":                 {kind: optionIgnore, warning: "curl failure/output behavior is not imported"},
	"--fail":             {kind: optionIgnore, warning: "curl failure/output behavior is not imported"},
	"--fail-with-body":   {kind: optionIgnore, warning: "curl failure/output behavior is not imported"},
	"-s":                 {kind: optionIgnore, warning: "curl terminal output behavior is not imported"},
	"--silent":           {kind: optionIgnore, warning: "curl terminal output behavior is not imported"},
	"-S":                 {kind: optionIgnore, warning: "curl terminal output behavior is not imported"},
	"--show-error":       {kind: optionIgnore, warning: "curl terminal output behavior is not imported"},
	"-v":                 {kind: optionIgnore, warning: "curl verbose output behavior is not imported"},
	"--verbose":          {kind: optionIgnore, warning: "curl verbose output behavior is not imported"},
	"-k":                 {kind: optionIgnore, warning: "TLS verification override is not imported"},
	"--insecure":         {kind: optionIgnore, warning: "TLS verification override is not imported"},
	"-x":                 {kind: optionIgnore, hasValue: true, warning: "proxy configuration is not imported"},
	"--proxy":            {kind: optionIgnore, hasValue: true, warning: "proxy configuration is not imported"},
	"--resolve":          {kind: optionIgnore, hasValue: true, warning: "DNS override is not imported"},
	"--connect-to":       {kind: optionIgnore, hasValue: true, warning: "connection routing override is not imported"},
	"--interface":        {kind: optionIgnore, hasValue: true, warning: "interface binding is not imported"},
	"--retry":            {kind: optionIgnore, hasValue: true, warning: "retry behavior is not imported"},
	"--retry-delay":      {kind: optionIgnore, hasValue: true, warning: "retry behavior is not imported"},
	"--connect-timeout":  {kind: optionIgnore, hasValue: true, warning: "timeout behavior is not imported"},
	"-m":                 {kind: optionIgnore, hasValue: true, warning: "timeout behavior is not imported"},
	"--max-time":         {kind: optionIgnore, hasValue: true, warning: "timeout behavior is not imported"},
	"-o":                 {kind: optionIgnore, hasValue: true, warning: "output file behavior is not imported"},
	"--output":           {kind: optionIgnore, hasValue: true, warning: "output file behavior is not imported"},
	"-c":                 {kind: optionIgnore, hasValue: true, warning: "cookie-jar persistence is not imported"},
	"--cookie-jar":       {kind: optionIgnore, hasValue: true, warning: "cookie-jar persistence is not imported"},
}

type formField struct {
	name  string
	value string
}

func parseCommand(command string) (*ImportResult, error) {
	tokens, err := shellWords(command)
	if err != nil {
		return nil, err
	}
	tokens, err = expandShortOptions(tokens)
	if err != nil {
		return nil, err
	}

	result := &ImportResult{Method: http.MethodGet, Headers: make(http.Header)}
	var bodyParts []string
	var queryParts []string
	var fields []formField
	bodyMode := ""
	explicitMethod := false
	useGet := false
	suppressedHeaders := make(map[string]bool)
	cert := CertificateSpec{}
	hasCertOptions := false

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token == "--" {
			if i+1 >= len(tokens) || result.URL != "" || i+2 != len(tokens) {
				return nil, errors.New("curl: -- must be followed by the only URL")
			}
			result.URL = tokens[i+1]
			break
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			if result.URL != "" {
				return nil, fmt.Errorf("curl: multiple URLs or unexpected positional argument %q", token)
			}
			result.URL = token
			continue
		}

		name, value, attached := splitLongOption(token)
		definition, found := supportedOptions[name]
		if !found {
			return nil, fmt.Errorf("curl: unsupported option %q", name)
		}
		if definition.hasValue && !attached {
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("curl: option %s requires a value", name)
			}
			i++
			value = tokens[i]
		}
		if !definition.hasValue && attached {
			return nil, fmt.Errorf("curl: option %s does not accept a value", name)
		}

		switch definition.kind {
		case optionHeader:
			if err := applyHeader(result, suppressedHeaders, value); err != nil {
				return nil, err
			}
		case optionMethod:
			result.Method = strings.ToUpper(strings.TrimSpace(value))
			explicitMethod = true
		case optionData, optionDataRaw, optionDataBinary:
			if len(fields) != 0 {
				return nil, errors.New("curl: multipart form and data options cannot be mixed")
			}
			if strings.HasPrefix(value, "@") && definition.kind != optionDataRaw {
				return nil, errors.New("curl: file/stdin-backed request bodies are not imported")
			}
			bodyMode = "data"
			bodyParts = append(bodyParts, value)
			upgradeSecurityTo(result, Review)
		case optionDataURLEncode:
			if len(fields) != 0 {
				return nil, errors.New("curl: multipart form and data options cannot be mixed")
			}
			encoded, encodeErr := encodeData(value)
			if encodeErr != nil {
				return nil, encodeErr
			}
			bodyMode = "data"
			bodyParts = append(bodyParts, encoded)
			upgradeSecurityTo(result, Review)
		case optionJSON:
			if len(fields) != 0 || strings.HasPrefix(value, "@") {
				return nil, errors.New("curl: file-backed JSON and mixed multipart bodies are not imported")
			}
			bodyMode = "json"
			bodyParts = append(bodyParts, value)
			upgradeSecurityTo(result, Review)
		case optionForm, optionFormString:
			if len(bodyParts) != 0 {
				return nil, errors.New("curl: multipart form and data options cannot be mixed")
			}
			field, fieldErr := parseFormField(value, definition.kind == optionFormString)
			if fieldErr != nil {
				return nil, fieldErr
			}
			fields = append(fields, field)
			upgradeSecurityTo(result, Review)
		case optionUser:
			result.Headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(value)))
			upgradeSecurityTo(result, Review)
		case optionCookie:
			if strings.HasPrefix(value, "@") {
				return nil, errors.New("curl: file-backed cookies are not imported")
			}
			result.Headers.Add("Cookie", value)
			upgradeSecurityTo(result, Review)
		case optionURL:
			if result.URL != "" {
				return nil, errors.New("curl: multiple URLs are not imported")
			}
			result.URL = value
		case optionURLQuery:
			encoded := ""
			if strings.HasPrefix(value, "+") {
				encoded = strings.TrimPrefix(value, "+")
			} else {
				var encodeErr error
				encoded, encodeErr = encodeData(value)
				if encodeErr != nil {
					return nil, fmt.Errorf("curl: --url-query: %w", encodeErr)
				}
			}
			queryParts = append(queryParts, encoded)
		case optionCert:
			cert.File = value
			hasCertOptions = true
			upgradeSecurityTo(result, Review)
		case optionCertType:
			cert.Type = strings.ToUpper(strings.TrimSpace(value))
			hasCertOptions = true
		case optionKey:
			cert.KeyFile = value
			hasCertOptions = true
		case optionCACert:
			cert.CAFile = value
			hasCertOptions = true
		case optionHead:
			result.Method = http.MethodHead
			explicitMethod = true
		case optionGet:
			useGet = true
		case optionUserAgent:
			result.Headers.Set("User-Agent", value)
		case optionReferer:
			result.Headers.Set("Referer", value)
		case optionIgnore:
			addWarning(result, definition.warning)
		}
	}

	if result.URL == "" {
		return nil, errors.New("curl: no URL found in command")
	}
	autoContentType := false
	autoAccept := false
	if len(fields) != 0 {
		if useGet {
			return nil, errors.New("curl: --get cannot be combined with multipart form data")
		}
		body, contentType, formErr := buildMultipartBody(command, fields)
		if formErr != nil {
			return nil, formErr
		}
		result.Body = body
		autoContentType = setDefaultHeader(result.Headers, suppressedHeaders, "Content-Type", contentType)
	} else {
		result.Body = strings.Join(bodyParts, "&")
		if result.Body != "" {
			contentType := "application/x-www-form-urlencoded"
			if bodyMode == "json" {
				contentType = "application/json"
				autoAccept = setDefaultHeader(result.Headers, suppressedHeaders, "Accept", "application/json")
			}
			autoContentType = setDefaultHeader(result.Headers, suppressedHeaders, "Content-Type", contentType)
		}
	}

	if useGet {
		if result.Body != "" {
			queryParts = append(queryParts, result.Body)
			result.Body = ""
			if autoContentType {
				result.Headers.Del("Content-Type")
			}
			if autoAccept {
				result.Headers.Del("Accept")
			}
		}
		if !explicitMethod {
			result.Method = http.MethodGet
		}
	} else if result.Body != "" && !explicitMethod {
		result.Method = http.MethodPost
	}
	if len(queryParts) != 0 {
		result.URL = appendQuery(result.URL, strings.Join(queryParts, "&"))
	}

	if hasCertOptions {
		upgradeSecurityTo(result, Review)
		finalizeCertificate(&cert, result)
		result.Certificate = &cert
	}
	if err := validateRequest(result); err != nil {
		return nil, err
	}
	return result, nil
}

func shellWords(command string) ([]string, error) {
	command = strings.TrimSpace(normalizeContinuations(command))
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "curl")
	if err != nil {
		return nil, fmt.Errorf("curl: shell syntax: %w", err)
	}
	if len(file.Stmts) != 1 {
		return nil, errors.New("curl: exactly one command is required")
	}
	stmt := file.Stmts[0]
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || stmt.Semicolon.IsValid() || len(stmt.Redirs) != 0 {
		return nil, errors.New("curl: shell control operators and redirections are not imported")
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) != 0 {
		return nil, errors.New("curl: only one simple command without assignments is accepted")
	}
	words := make([]string, 0, len(call.Args))
	for _, word := range call.Args {
		literal, literalErr := literalWord(word)
		if literalErr != nil {
			return nil, fmt.Errorf("curl: %w", literalErr)
		}
		words = append(words, literal)
	}
	if len(words) == 0 || (strings.ToLower(words[0]) != "curl" && strings.ToLower(words[0]) != "curl.exe") {
		return nil, errors.New("curl: command must start with curl or curl.exe")
	}
	return words[1:], nil
}

func normalizeContinuations(command string) string {
	var out strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
		}
		marker := !inSingle && (ch == '`' || (ch == '^' && !inDouble))
		if marker && i+1 < len(command) && command[i+1] == '\n' {
			out.WriteString("\\\n")
			i++
			continue
		}
		if marker && i+2 < len(command) && command[i+1] == '\r' && command[i+2] == '\n' {
			out.WriteString("\\\n")
			i += 2
			continue
		}
		if ch == '\\' && i+2 < len(command) && command[i+1] == '\r' && command[i+2] == '\n' && !inSingle {
			out.WriteString("\\\n")
			i += 2
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func literalWord(word *syntax.Word) (string, error) {
	var out strings.Builder
	var appendParts func([]syntax.WordPart, bool) error
	appendParts = func(parts []syntax.WordPart, doubleQuoted bool) error {
		for _, part := range parts {
			switch part := part.(type) {
			case *syntax.Lit:
				out.WriteString(decodeLiteral(part.Value, doubleQuoted))
			case *syntax.SglQuoted:
				if part.Dollar {
					return errors.New("ANSI-C quoted words are not imported")
				}
				out.WriteString(part.Value)
			case *syntax.DblQuoted:
				if part.Dollar {
					return errors.New("locale-translated quoted words are not imported")
				}
				if err := appendParts(part.Parts, true); err != nil {
					return err
				}
			default:
				return fmt.Errorf("shell expansion is not imported (%T)", part)
			}
		}
		return nil
	}
	if err := appendParts(word.Parts, false); err != nil {
		return "", err
	}
	return out.String(), nil
}

func decodeLiteral(value string, doubleQuoted bool) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 >= len(value) {
			out.WriteByte(value[i])
			continue
		}
		next := value[i+1]
		if !doubleQuoted || next == '$' || next == '`' || next == '"' || next == '\\' || next == '\n' {
			i++
			if next != '\n' {
				out.WriteByte(next)
			}
			continue
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func expandShortOptions(tokens []string) ([]string, error) {
	expanded := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if len(token) <= 2 || !strings.HasPrefix(token, "-") || strings.HasPrefix(token, "--") {
			expanded = append(expanded, token)
			continue
		}
		if _, exact := supportedOptions[token]; exact {
			expanded = append(expanded, token)
			continue
		}
		cluster := make([]string, 0, len(token)-1)
		valid := true
		for pos := 1; pos < len(token); pos++ {
			name := "-" + token[pos:pos+1]
			definition, found := supportedOptions[name]
			if !found {
				valid = false
				break
			}
			cluster = append(cluster, name)
			if definition.hasValue {
				if pos+1 < len(token) {
					cluster = append(cluster, token[pos+1:])
				}
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("curl: unsupported option %q", token)
		}
		expanded = append(expanded, cluster...)
	}
	return expanded, nil
}

func splitLongOption(token string) (name, value string, attached bool) {
	if strings.HasPrefix(token, "--") {
		name, value, attached = strings.Cut(token, "=")
		return name, value, attached
	}
	return token, "", false
}

func applyHeader(result *ImportResult, suppressed map[string]bool, value string) error {
	if strings.HasPrefix(value, "@") {
		return errors.New("curl: file-backed headers are not imported")
	}
	key, headerValue, found := strings.Cut(value, ":")
	forceEmpty := false
	if !found && strings.HasSuffix(value, ";") {
		key = strings.TrimSuffix(value, ";")
		headerValue = ""
		found = true
		forceEmpty = true
	}
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return fmt.Errorf("curl: invalid header %q", value)
	}
	if strings.TrimSpace(headerValue) == "" && !forceEmpty {
		result.Headers.Del(key)
		suppressed[strings.ToLower(key)] = true
		return nil
	}
	delete(suppressed, strings.ToLower(key))
	result.Headers.Add(key, strings.TrimSpace(headerValue))
	classifyHeader(key, result)
	return nil
}

func encodeData(value string) (string, error) {
	if strings.HasPrefix(value, "@") {
		return "", errors.New("curl: file/stdin-backed --data-urlencode is not imported")
	}
	if at := strings.IndexByte(value, '@'); at >= 0 && !strings.Contains(value[:at], "=") {
		return "", errors.New("curl: file-backed --data-urlencode is not imported")
	}
	if eq := strings.IndexByte(value, '='); eq >= 0 {
		if eq == 0 {
			return curlEscape(value[1:]), nil
		}
		return value[:eq+1] + curlEscape(value[eq+1:]), nil
	}
	return curlEscape(value), nil
}

func curlEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func parseFormField(value string, literal bool) (formField, error) {
	name, fieldValue, found := strings.Cut(value, "=")
	if !found || strings.TrimSpace(name) == "" {
		return formField{}, fmt.Errorf("curl: invalid multipart form field %q", value)
	}
	if !literal && (strings.HasPrefix(fieldValue, "@") || strings.HasPrefix(fieldValue, "<")) {
		return formField{}, errors.New("curl: file-backed multipart fields are not imported")
	}
	return formField{name: name, value: fieldValue}, nil
}

func buildMultipartBody(command string, fields []formField) (string, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	digest := sha256.Sum256([]byte(command))
	boundary := "quark-" + hex.EncodeToString(digest[:12])
	if err := writer.SetBoundary(boundary); err != nil {
		return "", "", fmt.Errorf("curl: multipart boundary: %w", err)
	}
	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return "", "", fmt.Errorf("curl: multipart field %q: %w", field.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return "", "", fmt.Errorf("curl: finalize multipart body: %w", err)
	}
	return body.String(), writer.FormDataContentType(), nil
}

func setDefaultHeader(headers http.Header, suppressed map[string]bool, key, value string) bool {
	if headers.Get(key) == "" && !suppressed[strings.ToLower(key)] {
		headers.Set(key, value)
		return true
	}
	return false
}

func appendQuery(rawURL, query string) string {
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + query
}

func finalizeCertificate(cert *CertificateSpec, result *ImportResult) {
	if cert.File != "" {
		if colon := strings.LastIndexByte(cert.File, ':'); colon > 1 {
			cert.File, cert.Password = cert.File[:colon], cert.File[colon+1:]
		}
		if cert.Type == "" {
			lower := strings.ToLower(cert.File)
			if strings.HasSuffix(lower, ".p12") || strings.HasSuffix(lower, ".pfx") {
				cert.Type = "P12"
			} else {
				cert.Type = "PEM"
			}
		}
	}
	if cert.File == "" && cert.KeyFile != "" {
		addWarning(result, "--key cannot be used without --cert")
	}
	if cert.Type != "" && cert.Type != "P12" && cert.Type != "PEM" {
		addWarning(result, fmt.Sprintf("certificate type %s is not supported by Quark", cert.Type))
	}
	if cert.Type == "P12" && cert.KeyFile != "" {
		addWarning(result, "--key is not used with a P12 certificate")
	}
}

func validateRequest(result *ImportResult) error {
	parsedURL, err := url.Parse(result.URL)
	if err != nil {
		return fmt.Errorf("curl: invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("curl: URL scheme %q is not supported", parsedURL.Scheme)
	}
	if parsedURL.Host == "" {
		return errors.New("curl: URL host is required")
	}
	request, err := http.NewRequest(result.Method, result.URL, strings.NewReader(result.Body))
	if err != nil {
		return fmt.Errorf("curl: invalid HTTP request: %w", err)
	}
	request.Header = result.Headers.Clone()
	return nil
}

func classifyHeader(key string, result *ImportResult) {
	switch strings.ToLower(key) {
	case "authorization", "cookie", "proxy-authorization", "x-api-key":
		upgradeSecurityTo(result, Review)
	}
}

func addWarning(result *ImportResult, warning string) {
	if warning == "" {
		return
	}
	for _, existing := range result.Warnings {
		if existing == warning {
			return
		}
	}
	result.Warnings = append(result.Warnings, warning)
	upgradeSecurityTo(result, Review)
}

func upgradeSecurityTo(result *ImportResult, level SecurityLevel) {
	if level > result.Security {
		result.Security = level
	}
}
