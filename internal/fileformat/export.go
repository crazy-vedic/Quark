package fileformat

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"

	"go.yaml.in/yaml/v3"

	"github.com/crazy-vedic/quark/internal/atomicfile"
	"github.com/crazy-vedic/quark/internal/domain"
)

// Source is what Load reads from the store.
type Source interface {
	ListCollections(ctx context.Context) ([]*domain.Collection, error)
	ListRequests(ctx context.Context, collectionID string) ([]*domain.Request, error)
	ListCollectionEnvironments(
		ctx context.Context,
		collectionID string,
	) ([]*domain.Environment, error)
}

// Load reads the collection with the given ID and all of its descendants.
func Load(ctx context.Context, src Source, collectionID string) (*Collection, error) {
	all, err := src.ListCollections(ctx)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	var root *domain.Collection
	children := make(map[string][]*domain.Collection)
	for _, c := range all {
		if c.ID == collectionID {
			root = c
		}
		children[c.ParentID] = append(children[c.ParentID], c)
	}
	if root == nil {
		return nil, fmt.Errorf("collection %q not found", collectionID)
	}
	return loadCollection(ctx, src, root, children, map[string]bool{})
}

func loadCollection(
	ctx context.Context,
	src Source,
	col *domain.Collection,
	children map[string][]*domain.Collection,
	seen map[string]bool,
) (*Collection, error) {
	if seen[col.ID] {
		return nil, fmt.Errorf("collection %q: cycle in collection tree", col.Name)
	}
	seen[col.ID] = true
	out := &Collection{Name: col.Name, Description: col.Description}
	if col.Meta != "" && col.Meta != "{}" {
		out.Meta = col.Meta
	}
	reqs, err := src.ListRequests(ctx, col.ID)
	if err != nil {
		return nil, fmt.Errorf("list requests of %q: %w", col.Name, err)
	}
	out.Requests = reqs
	envs, err := src.ListCollectionEnvironments(ctx, col.ID)
	if err != nil {
		return nil, fmt.Errorf("list environments of %q: %w", col.Name, err)
	}
	for _, env := range envs {
		vars, err := env.DecodeVars()
		if err != nil {
			return nil, fmt.Errorf("environment %q of %q: %w", env.Name, col.Name, err)
		}
		if env.Name == defaultEnvironment && len(vars) == 0 {
			// Every collection has one; an empty one carries nothing.
			continue
		}
		out.Environments = append(out.Environments, Environment{Name: env.Name, Vars: vars})
	}
	sort.SliceStable(out.Environments, func(i, j int) bool {
		return out.Environments[i].Name < out.Environments[j].Name
	})
	kids := append([]*domain.Collection(nil), children[col.ID]...)
	sort.SliceStable(kids, func(i, j int) bool { return kids[i].Name < kids[j].Name })
	for _, kid := range kids {
		child, err := loadCollection(ctx, src, kid, children, seen)
		if err != nil {
			return nil, err
		}
		out.Children = append(out.Children, child)
	}
	return out, nil
}

// Write writes c as a directory named after it inside dir and returns that
// directory's path. A previous export at the same path is updated in place:
// every file is replaced atomically, and files the new export no longer has,
// such as requests deleted since then, are removed. A directory at that path
// that is not a Quark export is left alone and an error is returned.
func Write(dir string, c *Collection) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	target := filepath.Join(dir, slug(c.Name))
	if info, err := os.Stat(target); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("%s exists and is not a directory", target)
		}
		if _, err := os.Stat(filepath.Join(target, collectionFileName)); err != nil {
			return "", fmt.Errorf(
				"%s exists and is not a Quark export (no %s); not overwriting it",
				target, collectionFileName,
			)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	files, err := renderCollection(c)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		full := filepath.Join(target, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return "", fmt.Errorf("create %s: %w", filepath.Dir(full), err)
		}
		if err := atomicfile.WriteFile(full, f.data, 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", full, err)
		}
	}
	if err := removeStale(target, files); err != nil {
		return "", fmt.Errorf("remove files of a previous export: %w", err)
	}
	return target, nil
}

// exportFile is one file of an export. rel is slash-separated and relative
// to the export directory.
type exportFile struct {
	rel  string
	data []byte
}

// renderCollection returns every file of c, in the order they are written,
// with collection.yaml of each directory last.
func renderCollection(c *Collection) ([]exportFile, error) {
	var files []exportFile
	if err := appendCollection(&files, "", c); err != nil {
		return nil, err
	}
	return files, nil
}

// appendCollection adds the files of c under prefix, a slash-separated
// directory ending in "/" or empty for the export root.
func appendCollection(out *[]exportFile, prefix string, c *Collection) error {
	names := newNameAllocator()
	meta := collectionFile{
		Format:      FormatVersion,
		Name:        c.Name,
		Description: c.Description,
		Meta:        c.Meta,
	}
	for _, req := range c.Requests {
		file := names.next(slug(req.Name), yamlExt)
		meta.Requests = append(meta.Requests, file)
		data, err := encodeYAML(requestToFile(req))
		if err != nil {
			return fmt.Errorf("encode request %q: %w", req.Name, err)
		}
		*out = append(*out, exportFile{rel: prefix + file, data: data})
	}
	for _, child := range c.Children {
		sub := names.next(slug(child.Name), "")
		meta.Collections = append(meta.Collections, sub)
		if err := appendCollection(out, prefix+sub+"/", child); err != nil {
			return err
		}
	}
	envNames := newNameAllocator()
	for _, env := range c.Environments {
		vars := env.Vars
		if vars == nil {
			vars = map[string]string{}
		}
		data, err := encodeYAML(environmentFile{Name: env.Name, Variables: vars})
		if err != nil {
			return fmt.Errorf("encode environment %q: %w", env.Name, err)
		}
		file := envNames.next(slug(env.Name), yamlExt)
		*out = append(*out, exportFile{rel: prefix + environmentsDir + "/" + file, data: data})
	}
	data, err := encodeYAML(meta)
	if err != nil {
		return fmt.Errorf("encode collection %q: %w", c.Name, err)
	}
	*out = append(*out, exportFile{rel: prefix + collectionFileName, data: data})
	return nil
}

// removeStale deletes everything under target that is not one of files or a
// directory holding one of them.
func removeStale(target string, files []exportFile) error {
	keep := map[string]bool{}
	for _, f := range files {
		for p := f.rel; p != "." && p != ""; p = path.Dir(p) {
			keep[p] = true
		}
	}
	var stale []string
	err := filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(target, p)
		if err != nil {
			return err
		}
		if rel == "." || keep[filepath.ToSlash(rel)] {
			return nil
		}
		stale = append(stale, p)
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, p := range stale {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

func requestToFile(req *domain.Request) requestFile {
	out := requestFile{
		Name: req.Name, Method: req.Method, URL: req.URL, Body: req.Body,
		Disabled: !req.Enabled,
	}
	if headers, err := domain.ParseHeaders(req.Headers); err != nil {
		out.HeadersJSON = req.Headers
	} else if len(headers) > 0 {
		out.Headers = make(map[string]yaml.Node, len(headers))
		for key, values := range headers {
			out.Headers[key] = headerNode(values)
		}
	}
	authType := domain.NormalizeAuthType(req.AuthType)
	if authType == domain.AuthTypeNone {
		return out
	}
	auth := &authFile{Type: authType}
	cfg, err := domain.ParseAuthConfig(req.AuthConfig)
	if err != nil {
		auth.ConfigJSON = req.AuthConfig
	} else {
		auth.Token = cfg.Token
		auth.Username = cfg.Username
		auth.Password = cfg.Password
		auth.Name = cfg.Name
		auth.Value = cfg.Value
		if authType == domain.AuthTypeAPIKey {
			auth.In = cfg.In
		}
	}
	out.Auth = auth
	return out
}

func headerNode(values []string) yaml.Node {
	if len(values) == 1 {
		return yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: values[0]}
	}
	seq := yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v})
	}
	return seq
}
