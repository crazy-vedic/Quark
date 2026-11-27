// Package fileformat writes Quark collections as a tree of human-readable
// YAML files and reads them back, so collections can be reviewed and
// versioned in git.
//
// Layout of one exported collection:
//
//	<collection-slug>/
//	  collection.yaml          name, description, and the files this export wrote:
//	                           requests, sub-collections and environments, in order
//	  <request-slug>.yaml      one file per request
//	  environments/<env>.yaml  one file per environment of this collection
//	  <sub-collection-slug>/   nested collections, same layout
//
// Output is deterministic: no IDs or timestamps are written, map keys are
// sorted, and file names are derived from entity names in a fixed order.
package fileformat

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/crazy-vedic/quark/internal/domain"
)

// FormatVersion is written to every collection.yaml.
const FormatVersion = 1

const (
	collectionFileName = "collection.yaml"
	environmentsDir    = "environments"
	yamlExt            = ".yaml"
	maxSlugRunes       = 60
	defaultEnvironment = "default"
)

// Collection is one collection and everything below it, independent of
// storage IDs.
type Collection struct {
	Name         string
	Description  string
	Meta         string
	Requests     []*domain.Request // in display order
	Environments []Environment     // sorted by name
	Children     []*Collection     // sorted by name
}

// Environment is a named set of variables owned by a collection.
type Environment struct {
	Name string
	Vars map[string]string
}

// CountRequests returns the number of requests in c and its descendants.
func (c *Collection) CountRequests() int {
	n := len(c.Requests)
	for _, child := range c.Children {
		n += child.CountRequests()
	}
	return n
}

type collectionFile struct {
	Format      int      `yaml:"format"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Meta        string   `yaml:"meta,omitempty"`
	Requests    []string `yaml:"requests,omitempty"`
	Collections []string `yaml:"collections,omitempty"`
	// Environments lists the file names in environments/. An export removes
	// only the files its previous export listed, so every file it writes must
	// be listed here.
	Environments []string `yaml:"environments,omitempty"`
}

type requestFile struct {
	Name   string `yaml:"name"`
	Method string `yaml:"method"`
	URL    string `yaml:"url"`
	// Headers values are a string, or a list of strings for repeated headers.
	Headers map[string]yaml.Node `yaml:"headers,omitempty"`
	// HeadersJSON holds stored headers that are not valid header JSON, so an
	// export never loses them.
	HeadersJSON string    `yaml:"headers_json,omitempty"`
	Auth        *authFile `yaml:"auth,omitempty"`
	Body        string    `yaml:"body,omitempty"`
	// Disabled is written only for requests that are switched off, so
	// enabled requests (the default) stay out of the files.
	Disabled bool `yaml:"disabled,omitempty"`
}

type authFile struct {
	Type     string `yaml:"type"`
	Token    string `yaml:"token,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	In       string `yaml:"in,omitempty"`
	Name     string `yaml:"name,omitempty"`
	Value    string `yaml:"value,omitempty"`
	// ConfigJSON holds a stored auth config that is not valid JSON.
	ConfigJSON string `yaml:"config_json,omitempty"`
}

type environmentFile struct {
	Name      string            `yaml:"name"`
	Variables map[string]string `yaml:"variables"`
}

// slug turns a name into a lower-case file name stem made of letters,
// digits and single dashes.
func slug(name string) string {
	var b strings.Builder
	dash := false
	runes := 0
	for _, r := range strings.ToLower(name) {
		if runes >= maxSlugRunes {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			runes++
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
			runes++
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "item"
	}
	return s
}

// nameAllocator hands out unique file names within one directory. Names are
// compared case-insensitively so exports work on case-insensitive file
// systems.
type nameAllocator struct{ used map[string]bool }

func newNameAllocator() *nameAllocator {
	return &nameAllocator{used: map[string]bool{
		collectionFileName: true,
		environmentsDir:    true,
	}}
}

func (a *nameAllocator) next(stem, ext string) string {
	candidate := stem + ext
	for i := 2; a.used[strings.ToLower(candidate)]; i++ {
		candidate = stem + "-" + strconv.Itoa(i) + ext
	}
	a.used[strings.ToLower(candidate)] = true
	return candidate
}

// encodeYAML marshals v with two-space indentation and writes every
// multi-line string as a literal block scalar so bodies stay readable and
// diff line by line.
func encodeYAML(v any) ([]byte, error) {
	var node yaml.Node
	if err := node.Encode(v); err != nil {
		return nil, err
	}
	useLiteralBlocks(&node)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func useLiteralBlocks(node *yaml.Node) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && strings.Contains(node.Value, "\n") {
		node.Style = yaml.LiteralStyle
	}
	for _, child := range node.Content {
		useLiteralBlocks(child)
	}
}
