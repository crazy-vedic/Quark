package fileformat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/crazy-vedic/quark/internal/domain"
)

// IsCollectionDir reports whether dir holds an exported collection.
func IsCollectionDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, collectionFileName))
	return err == nil && !info.IsDir()
}

// FindCollections returns the exported collections to import from dir: dir
// itself when it is a collection directory, otherwise each immediate
// sub-directory that is one, sorted by name.
func FindCollections(dir string) ([]string, error) {
	if IsCollectionDir(dir) {
		return []string{dir}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		sub := filepath.Join(dir, entry.Name())
		if IsCollectionDir(sub) {
			out = append(out, sub)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no %s found in %s or its sub-directories", collectionFileName, dir)
	}
	sort.Strings(out)
	return out, nil
}

// Read loads the exported collection in dir. Request files and
// sub-collection directories that collection.yaml does not list (for
// example ones added by hand) are included after the listed ones, sorted by
// file name.
func Read(dir string) (*Collection, error) {
	var meta collectionFile
	if err := decodeFile(filepath.Join(dir, collectionFileName), &meta); err != nil {
		return nil, err
	}
	if meta.Format > FormatVersion {
		return nil, fmt.Errorf(
			"%s: format %d is newer than this Quark supports (%d)",
			filepath.Join(dir, collectionFileName), meta.Format, FormatVersion,
		)
	}
	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = filepath.Base(dir)
	}
	out := &Collection{Name: name, Description: meta.Description, Meta: meta.Meta}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var extraFiles, extraDirs []string
	listedFiles := setOf(meta.Requests)
	listedDirs := setOf(meta.Collections)
	for _, entry := range entries {
		entryName := entry.Name()
		if strings.HasPrefix(entryName, ".") {
			continue
		}
		switch {
		case entry.IsDir():
			if entryName != environmentsDir && !listedDirs[entryName] &&
				IsCollectionDir(filepath.Join(dir, entryName)) {
				extraDirs = append(extraDirs, entryName)
			}
		case entryName != collectionFileName && strings.HasSuffix(entryName, yamlExt) &&
			!listedFiles[entryName]:
			extraFiles = append(extraFiles, entryName)
		}
	}

	for _, file := range append(append([]string(nil), meta.Requests...), extraFiles...) {
		if err := checkEntryName(dir, file); err != nil {
			return nil, err
		}
		req, err := readRequest(filepath.Join(dir, file))
		if err != nil {
			return nil, err
		}
		out.Requests = append(out.Requests, req)
	}
	for _, sub := range append(append([]string(nil), meta.Collections...), extraDirs...) {
		if err := checkEntryName(dir, sub); err != nil {
			return nil, err
		}
		child, err := Read(filepath.Join(dir, sub))
		if err != nil {
			return nil, err
		}
		out.Children = append(out.Children, child)
	}
	envs, err := readEnvironments(filepath.Join(dir, environmentsDir))
	if err != nil {
		return nil, err
	}
	out.Environments = envs
	return out, nil
}

func checkEntryName(dir, name string) error {
	if name == "" || name != filepath.Base(name) || name == ".." {
		return fmt.Errorf("%s: invalid entry %q in %s", dir, name, collectionFileName)
	}
	return nil
}

func setOf(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

func decodeFile(path string, v any) error {
	data, err := os.ReadFile(path) //nolint:gosec // The user chose the directory to import.
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func readRequest(path string) (*domain.Request, error) {
	var file requestFile
	if err := decodeFile(path, &file); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(file.Name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), yamlExt)
	}
	method := strings.ToUpper(strings.TrimSpace(file.Method))
	if method == "" {
		method = "GET"
	}
	req := &domain.Request{
		Name:    name,
		Method:  method,
		URL:     file.URL,
		Body:    file.Body,
		Enabled: !file.Disabled,
	}
	headers, err := headersJSON(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	req.Headers = headers
	if file.Auth != nil {
		req.AuthType = domain.NormalizeAuthType(file.Auth.Type)
		if file.Auth.ConfigJSON != "" {
			req.AuthConfig = file.Auth.ConfigJSON
		} else {
			req.AuthConfig = domain.MustAuthConfigJSON(domain.AuthConfig{
				Token:    file.Auth.Token,
				Username: file.Auth.Username,
				Password: file.Auth.Password,
				In:       file.Auth.In,
				Name:     file.Auth.Name,
				Value:    file.Auth.Value,
			})
		}
	}
	return req, nil
}

func headersJSON(file requestFile) (string, error) {
	if file.HeadersJSON != "" {
		if len(file.Headers) > 0 {
			return "", fmt.Errorf("use either headers or headers_json, not both")
		}
		return file.HeadersJSON, nil
	}
	if len(file.Headers) == 0 {
		return "{}", nil
	}
	headers := make(map[string]any, len(file.Headers))
	for key, node := range file.Headers {
		switch node.Kind {
		case yaml.ScalarNode:
			headers[key] = node.Value
		case yaml.SequenceNode:
			values := make([]string, 0, len(node.Content))
			for _, item := range node.Content {
				if item.Kind != yaml.ScalarNode {
					return "", fmt.Errorf("header %q: list items must be strings", key)
				}
				values = append(values, item.Value)
			}
			headers[key] = values
		default:
			return "", fmt.Errorf("header %q: value must be a string or a list of strings", key)
		}
	}
	data, err := json.Marshal(headers)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readEnvironments(dir string) ([]Environment, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Environment
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), yamlExt) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		var file environmentFile
		if err := decodeFile(path, &file); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(file.Name)
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), yamlExt)
		}
		vars := file.Variables
		if vars == nil {
			vars = map[string]string{}
		}
		out = append(out, Environment{Name: name, Vars: vars})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
