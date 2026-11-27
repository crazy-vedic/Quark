// Package openapi converts an OpenAPI 3.x document (JSON or YAML) into a tree
// of Quark requests. Tags become folders, server URLs become the {{baseUrl}}
// variable, path parameters become {{name}} placeholders, and request bodies
// are generated from examples or schemas.
package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/crazy-vedic/quark/internal/domain"
)

// BaseURLVar is the variable that every imported URL starts with.
const BaseURLVar = "baseUrl"

// maxSchemaDepth bounds example generation for recursive schemas.
const maxSchemaDepth = 6

// maxRefHops bounds chains of $ref to $ref.
const maxRefHops = 16

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

var pathParamPattern = regexp.MustCompile(`\{([^{}]+)\}`)

// Collection is one folder of imported requests. Only the root folder has
// Description and Vars.
type Collection struct {
	Name        string
	Description string
	Requests    []*domain.Request
	Children    []*Collection
	// Vars are the environment variables of the root folder: baseUrl, example
	// values of path parameters and empty placeholders for credentials.
	Vars map[string]string
}

// CountRequests returns the number of requests in c and its folders.
func (c *Collection) CountRequests() int {
	n := len(c.Requests)
	for _, child := range c.Children {
		n += child.CountRequests()
	}
	return n
}

// Result is a converted document and the constructs that were skipped or
// could not be mapped exactly.
type Result struct {
	Collection *Collection
	Warnings   []string
}

// Convert parses data as an OpenAPI 3.x document and converts it. Swagger 2.0
// and documents without operations are rejected.
func Convert(data []byte) (*Result, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse document: %w", err)
	}
	doc, ok := normalize(raw).(map[string]any)
	if !ok {
		return nil, errors.New("not an OpenAPI document: the top level must be an object")
	}
	if _, ok := doc["swagger"]; ok {
		return nil, errors.New(
			"swagger 2.0 is not supported; convert the document to OpenAPI 3 first",
		)
	}
	if version := str(doc["openapi"]); !isOpenAPI3(version) {
		return nil, fmt.Errorf("unsupported OpenAPI version %q: only 3.x is supported", version)
	}

	c := &converter{
		doc:         doc,
		vars:        map[string]string{},
		unsupported: map[string]string{},
		seen:        map[string]bool{},
	}
	root := &Collection{Name: "OpenAPI", Vars: c.vars}
	if info := asMap(doc["info"]); info != nil {
		if title := strings.TrimSpace(str(info["title"])); title != "" {
			root.Name = title
		}
		root.Description = strings.TrimSpace(str(info["description"]))
	}
	c.vars[BaseURLVar] = c.serverURL()

	folders := map[string]*Collection{}
	paths := asMap(doc["paths"])
	operations := 0
	for _, path := range sortedKeys(paths) {
		item := c.deref(paths[path])
		if item == nil {
			continue
		}
		for _, method := range httpMethods {
			op := asMap(item[method])
			if op == nil {
				continue
			}
			operations++
			folder := root
			if tag := firstTag(op); tag != "" {
				folder = folders[tag]
				if folder == nil {
					folder = &Collection{Name: tag}
					folders[tag] = folder
					root.Children = append(root.Children, folder)
				}
			}
			req := c.operation(path, method, op, item)
			req.Name = uniqueIn(folder, req.Name)
			folder.Requests = append(folder.Requests, req)
		}
	}
	if operations == 0 {
		return nil, errors.New("no operations found in paths")
	}
	sort.SliceStable(root.Children, func(i, j int) bool {
		return root.Children[i].Name < root.Children[j].Name
	})
	c.finish()
	return &Result{Collection: root, Warnings: c.warnings}, nil
}

type converter struct {
	doc      map[string]any
	vars     map[string]string
	warnings []string
	seen     map[string]bool
	// unsupported maps security scheme names that were referenced but cannot
	// be mapped to their type.
	unsupported    map[string]string
	warnedServers  bool
	warnedCallback bool
}

func (c *converter) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if !c.seen[msg] {
		c.seen[msg] = true
		c.warnings = append(c.warnings, msg)
	}
}

// finish adds the warnings that apply to the whole document.
func (c *converter) finish() {
	if _, ok := c.doc["webhooks"]; ok {
		c.warn("webhooks are not imported")
	}
	for _, name := range sortedStrings(c.unsupported) {
		c.warn("security scheme %q (%s) is not supported; "+
			"requests that use it are sent without auth", name, c.unsupported[name])
	}
}

func (c *converter) operation(
	path, method string,
	op, item map[string]any,
) *domain.Request {
	label := strings.ToUpper(method) + " " + path
	if _, ok := op["servers"]; ok && !c.warnedServers {
		c.warnedServers = true
		c.warn("per-operation servers are ignored; every request uses the first server")
	}
	if _, ok := op["callbacks"]; ok && !c.warnedCallback {
		c.warnedCallback = true
		c.warn("callbacks are not imported")
	}

	headers := map[string]string{}
	query := url.Values{}
	for _, p := range c.parameters(item, op) {
		pname := str(p["name"])
		switch str(p["in"]) {
		case "path":
			c.setVar(pname, c.paramValue(p))
		case "query":
			if isTrue(p["required"]) {
				query.Set(pname, c.paramValue(p))
			}
		case "header":
			if isTrue(p["required"]) {
				headers[pname] = c.paramValue(p)
			}
		case "cookie":
			c.warn("cookie parameters are not imported")
		}
	}
	for _, match := range pathParamPattern.FindAllStringSubmatch(path, -1) {
		c.setVar(match[1], "")
	}

	target := "{{" + BaseURLVar + "}}" + pathParamPattern.ReplaceAllString(path, "{{$1}}")
	if q := query.Encode(); q != "" {
		target += "?" + q
	}

	req := &domain.Request{
		Name:    operationName(op, label),
		Method:  strings.ToUpper(method),
		URL:     target,
		Body:    c.requestBody(label, op, headers),
		Enabled: true,
	}
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		c.warn("%s: headers not imported: %v", label, err)
		headersJSON = []byte("{}")
	}
	req.Headers = string(headersJSON)
	req.AuthType, req.AuthConfig = c.authFor(op)
	return req
}

func operationName(op map[string]any, label string) string {
	for _, key := range []string{"summary", "operationId"} {
		if s := strings.TrimSpace(str(op[key])); s != "" {
			return s
		}
	}
	return label
}

// parameters returns the parameters of an operation, merged with the ones
// its path item declares. An operation parameter replaces a path-level one
// with the same name and location.
func (c *converter) parameters(item, op map[string]any) []map[string]any {
	type key struct{ in, name string }
	var out []map[string]any
	index := map[key]int{}
	add := func(raw any) {
		p := c.deref(raw)
		if p == nil {
			return
		}
		k := key{str(p["in"]), str(p["name"])}
		if i, ok := index[k]; ok {
			out[i] = p
			return
		}
		index[k] = len(out)
		out = append(out, p)
	}
	for _, raw := range sliceOf(item["parameters"]) {
		add(raw)
	}
	for _, raw := range sliceOf(op["parameters"]) {
		add(raw)
	}
	return out
}

// paramValue returns the example value of a parameter, or "" when it has none.
func (c *converter) paramValue(p map[string]any) string {
	var v any
	switch {
	case p["example"] != nil:
		v = p["example"]
	case len(asMap(p["examples"])) > 0:
		examples := asMap(p["examples"])
		if first := c.deref(examples[sortedKeys(examples)[0]]); first != nil {
			v = first["value"]
		}
	default:
		v = c.example(p["schema"], 0)
	}
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, str(item))
		}
		return strings.Join(parts, ",")
	case map[string]any:
		data, _ := json.Marshal(x)
		return string(data)
	default:
		return fmt.Sprint(x)
	}
}

// setVar sets an environment variable unless an earlier operation already did.
func (c *converter) setVar(name, value string) {
	if name == "" || name == BaseURLVar {
		return
	}
	if _, ok := c.vars[name]; !ok {
		c.vars[name] = value
	}
}

// requestBody returns the example body of an operation and sets its
// Content-Type header. Only JSON bodies are generated.
func (c *converter) requestBody(label string, op map[string]any, headers map[string]string) string {
	rb := c.deref(op["requestBody"])
	if rb == nil {
		return ""
	}
	mediaType, mt := pickMedia(asMap(rb["content"]))
	if mediaType == "" {
		return ""
	}
	headers["Content-Type"] = mediaType
	if !isJSONMedia(mediaType) {
		c.warn("%s: %s request bodies are not generated; fill in the body", label, mediaType)
		return ""
	}
	ex, ok := c.mediaExample(mt)
	if !ok {
		return ""
	}
	data, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		c.warn("%s: example body not generated: %v", label, err)
		return ""
	}
	return string(data)
}

func (c *converter) mediaExample(mt map[string]any) (any, bool) {
	if ex, ok := mt["example"]; ok {
		return ex, true
	}
	examples := asMap(mt["examples"])
	for _, key := range sortedKeys(examples) {
		if ex := c.deref(examples[key]); ex != nil {
			if v, ok := ex["value"]; ok {
				return v, true
			}
		}
	}
	v := c.example(mt["schema"], 0)
	return v, v != nil
}

// example builds a sample value for a schema from its example, default or
// enum fields, or else from its type.
func (c *converter) example(v any, depth int) any {
	if depth > maxSchemaDepth {
		return nil
	}
	s := c.deref(v)
	if s == nil {
		return nil
	}
	if ex, ok := s["example"]; ok {
		return ex
	}
	if def, ok := s["default"]; ok {
		return def
	}
	if enum := sliceOf(s["enum"]); len(enum) > 0 {
		return enum[0]
	}
	if all := sliceOf(s["allOf"]); len(all) > 0 && s["type"] == nil {
		merged := map[string]any{}
		for _, part := range all {
			if m, ok := c.example(part, depth+1).(map[string]any); ok {
				for k, val := range m {
					merged[k] = val
				}
			}
		}
		return merged
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if opts := sliceOf(s[key]); len(opts) > 0 {
			return c.example(opts[0], depth+1)
		}
	}
	typ := str(s["type"])
	if typ == "" {
		if _, ok := s["properties"]; ok {
			typ = "object"
		} else if _, ok := s["items"]; ok {
			typ = "array"
		}
	}
	switch typ {
	case "object":
		obj := map[string]any{}
		props := asMap(s["properties"])
		for name, prop := range props {
			obj[name] = c.example(prop, depth+1)
		}
		return obj
	case "array":
		if s["items"] == nil {
			return []any{}
		}
		return []any{c.example(s["items"], depth+1)}
	case "string":
		return ""
	case "integer", "number":
		return 0
	case "boolean":
		return false
	}
	return nil
}

// authFor maps the security requirement of an operation to a Quark auth
// type. The first requirement whose schemes are all supported wins; an empty
// requirement means anonymous access.
func (c *converter) authFor(op map[string]any) (string, string) {
	raw, ok := op["security"]
	if !ok {
		raw = c.doc["security"]
	}
	for _, r := range sliceOf(raw) {
		req := asMap(r)
		if len(req) == 0 {
			return domain.AuthTypeNone, ""
		}
		authType, cfg, ok := c.requirementAuth(req)
		if ok {
			return authType, domain.MustAuthConfigJSON(cfg)
		}
	}
	return domain.AuthTypeNone, ""
}

func (c *converter) requirementAuth(req map[string]any) (string, domain.AuthConfig, bool) {
	if len(req) != 1 {
		c.warn("security requirements that combine several schemes are not supported; " +
			"requests that use them are sent without auth")
		return "", domain.AuthConfig{}, false
	}
	var authType string
	var cfg domain.AuthConfig
	for _, name := range sortedKeys(req) {
		scheme := c.securityScheme(name)
		if scheme == nil {
			return "", domain.AuthConfig{}, false
		}
		typ := str(scheme["type"])
		switch {
		case typ == "http" && strings.EqualFold(str(scheme["scheme"]), "basic"):
			authType = domain.AuthTypeBasic
			cfg.Username = "{{username}}"
			cfg.Password = "{{password}}"
			c.setVar("username", "")
			c.setVar("password", "")
		case typ == "http" && strings.EqualFold(str(scheme["scheme"]), "bearer"):
			authType = domain.AuthTypeBearer
			cfg.Token = "{{token}}"
			c.setVar("token", "")
		case typ == "apiKey" && (str(scheme["in"]) == domain.AuthAPIKeyInHeader ||
			str(scheme["in"]) == domain.AuthAPIKeyInQuery):
			authType = domain.AuthTypeAPIKey
			cfg.Name = str(scheme["name"])
			cfg.In = str(scheme["in"])
			cfg.Value = "{{apiKey}}"
			c.setVar("apiKey", "")
		default:
			c.unsupported[name] = typ
			return "", domain.AuthConfig{}, false
		}
	}
	return authType, cfg, authType != ""
}

func (c *converter) securityScheme(name string) map[string]any {
	schemes := asMap(asMap(asMap(c.doc["components"])["securitySchemes"]))
	return c.deref(schemes[name])
}

// serverURL returns the first server with its variables filled in, without a
// trailing slash, and records a warning when it cannot be used as is.
func (c *converter) serverURL() string {
	servers := sliceOf(c.doc["servers"])
	if len(servers) == 0 {
		c.warn("document has no servers; %s is empty, set it in the environment", BaseURLVar)
		return ""
	}
	server := c.deref(servers[0])
	u := str(server["url"])
	vars := asMap(server["variables"])
	for _, name := range sortedKeys(vars) {
		def := str(c.deref(vars[name])["default"])
		u = strings.ReplaceAll(u, "{"+name+"}", def)
	}
	if len(servers) > 1 {
		c.warn("only the first server (%s) is used as %s", u, BaseURLVar)
	}
	if !strings.Contains(u, "://") {
		c.warn("server URL %q is relative; set %s in the environment", u, BaseURLVar)
	}
	return strings.TrimSuffix(u, "/")
}

// deref follows local $ref pointers and returns the object they lead to, or
// nil when v is not an object or the reference cannot be followed.
func (c *converter) deref(v any) map[string]any {
	for range maxRefHops {
		m := asMap(v)
		if m == nil {
			return nil
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		v = c.lookup(ref)
		if v == nil {
			return nil
		}
	}
	return nil
}

func (c *converter) lookup(ref string) any {
	if !strings.HasPrefix(ref, "#/") {
		c.warn("external $ref %q is not followed", ref)
		return nil
	}
	var cur any = c.doc
	for _, part := range strings.Split(ref[2:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				c.warn("unresolved $ref %q", ref)
				return nil
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				c.warn("unresolved $ref %q", ref)
				return nil
			}
			cur = node[idx]
		default:
			c.warn("unresolved $ref %q", ref)
			return nil
		}
	}
	return cur
}

// uniqueIn returns name, or "name (N)" with the smallest N not used by a
// request already in folder.
func uniqueIn(folder *Collection, name string) string {
	used := func(candidate string) bool {
		for _, r := range folder.Requests {
			if r.Name == candidate {
				return true
			}
		}
		return false
	}
	candidate := name
	for i := 1; used(candidate); i++ {
		candidate = fmt.Sprintf("%s (%d)", name, i)
	}
	return candidate
}

func firstTag(op map[string]any) string {
	for _, t := range sliceOf(op["tags"]) {
		if s := strings.TrimSpace(str(t)); s != "" {
			return s
		}
	}
	return ""
}

func pickMedia(content map[string]any) (string, map[string]any) {
	keys := sortedKeys(content)
	for _, k := range keys {
		if k == "application/json" {
			return k, asMap(content[k])
		}
	}
	for _, k := range keys {
		if isJSONMedia(k) {
			return k, asMap(content[k])
		}
	}
	if len(keys) > 0 {
		return keys[0], asMap(content[keys[0]])
	}
	return "", nil
}

func isJSONMedia(mediaType string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0]))
	return base == "application/json" || strings.HasSuffix(base, "+json")
}

func isOpenAPI3(version string) bool {
	return version == "3" || strings.HasPrefix(version, "3.")
}

// normalize rewrites every mapping to map[string]any with string keys, so
// callers need only one map type.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalize(val)
		}
		return out
	}
	return v
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func sliceOf(v any) []any {
	s, _ := v.([]any)
	return s
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func isTrue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrings(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
