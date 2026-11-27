package postmanimport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// importBulk imports every collection of a Postman bulk export directory and
// then the environments found in its environment/ subdirectory.
func (r *run) importBulk(ctx context.Context, dir string) (Result, error) {
	files, err := r.collectionFiles(dir)
	if err != nil {
		return Result{Bulk: true}, err
	}

	// A Postman export has no reliable collection-to-environment binding, so
	// standalone environments are preserved as selectable environments on every
	// imported root collection. Explicit Postman globals retain their broader
	// scope by importing into Quark's canonical Global environment.
	parsedEnvironments := parseEnvironmentsInDir(dir, r.logger)

	result := Result{Bulk: true}
	for _, f := range files {
		result.Collections = append(result.Collections, r.importFile(ctx, f))
	}

	result.Environments = r.importEnvironments(ctx, parsedEnvironments, result.Collections)
	return result, nil
}

// collectionFiles lists the collection files of a bulk export, preferring the
// archive.json manifest and falling back to scanning collection/.
func (r *run) collectionFiles(dir string) ([]string, error) {
	logger := r.logger
	var files []string

	archivePath := filepath.Join(dir, "archive.json")
	if data, err := os.ReadFile(archivePath); err == nil {
		var manifest struct {
			Collection map[string]bool `json:"collection"`
		}
		if err := json.Unmarshal(data, &manifest); err == nil {
			logger.Logf("archive.json manifest: %d collections", len(manifest.Collection))
			for id := range manifest.Collection {
				if !safeManifestID(id) {
					logger.Logf("manifest rejected unsafe id=%q", id)
					continue
				}
				p := filepath.Join(dir, "collection", id+".json")
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					files = append(files, p)
				} else {
					logger.Logf("manifest file missing id=%s path=%s", id, p)
				}
			}
		} else {
			logger.Logf("archive.json parse failed: %v", err)
		}
	} else {
		logger.Logf("archive.json not found, scanning collection/")
	}

	if len(files) == 0 {
		collDir := filepath.Join(dir, "collection")
		entries, err := os.ReadDir(collDir)
		if err != nil {
			logger.Logf("scan collection/ failed: %v", err)
			return nil, fmt.Errorf(
				"not a valid Postman bulk export: missing collection/ directory: %w",
				err,
			)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			files = append(files, filepath.Join(collDir, entry.Name()))
		}
	}

	if len(files) == 0 {
		logger.Logf("no collection files found in %s", dir)
		return nil, fmt.Errorf("no collection files found in %q", dir)
	}
	sort.Strings(files)
	logger.Logf("found %d collection files", len(files))
	return files, nil
}

// safeManifestID rejects manifest IDs that contain path traversal characters.
func safeManifestID(id string) bool {
	if id == "" {
		return false
	}
	if strings.ContainsAny(id, `/\`) {
		return false
	}
	return !strings.Contains(id, "..")
}
