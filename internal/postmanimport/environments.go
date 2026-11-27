package postmanimport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/postman"
)

const globalEnvironmentDisplayName = "Global"

type parsedEnvironmentFile struct {
	filename string
	name     string
	scope    string
	vars     map[string]string
}

type parsedEnvironmentResult struct {
	files    []parsedEnvironmentFile
	errors   []string
	warnings []string
}

// importEnvironments writes parsed Postman environments: globals merge into
// Quark's Global environment, named environments are copied to every root
// collection that was written in this import.
func (r *run) importEnvironments(
	ctx context.Context,
	parsed parsedEnvironmentResult,
	collections []CollectionResult,
) EnvironmentResult {
	result := EnvironmentResult{
		Errors:   append([]string(nil), parsed.errors...),
		Warnings: append([]string(nil), parsed.warnings...),
	}
	var globals, collectionEnvironments []parsedEnvironmentFile
	for _, file := range parsed.files {
		if strings.EqualFold(file.scope, "global") {
			globals = append(globals, file)
		} else {
			collectionEnvironments = append(collectionEnvironments, file)
		}
	}
	if len(globals) > 0 {
		warnings, err := mergeParsedEnvironmentsIntoGlobal(ctx, r.st, globals, r.logger)
		result.Warnings = append(result.Warnings, warnings...)
		if err != nil {
			errMsg := fmt.Sprintf("save Postman globals: %v", err)
			r.logger.Logf("%s", errMsg)
			result.Errors = append(result.Errors, errMsg)
		} else {
			result.Imported += len(globals)
		}
	}
	if len(collectionEnvironments) > 0 {
		for _, collection := range collections {
			if collection.Err != nil || collection.ID == "" {
				continue
			}
			imported, warnings, err := importParsedEnvironmentsForCollection(
				ctx, r.st, collection.ID, collectionEnvironments,
			)
			result.Imported += imported
			result.Warnings = append(result.Warnings, warnings...)
			if err != nil {
				errMsg := fmt.Sprintf(
					"%s: save standalone environments: %v",
					collection.Name,
					err,
				)
				r.logger.Logf("%s", errMsg)
				result.Errors = append(result.Errors, errMsg)
			}
		}
	}
	sort.Strings(result.Errors)
	sort.Strings(result.Warnings)
	return result
}

// importParsedEnvironmentsForCollection preserves Postman environments as
// collection-owned selectable environments. Existing local environments are
// never overwritten during an import; their values win and a redacted warning
// explains why a source environment was not replaced.
func importParsedEnvironmentsForCollection(
	ctx context.Context,
	st Store,
	collectionID string,
	files []parsedEnvironmentFile,
) (int, []string, error) {
	existing, err := st.ListCollectionEnvironments(ctx, collectionID)
	if err != nil {
		return 0, nil, fmt.Errorf("list existing environments: %w", err)
	}
	byName := make(map[string]*domain.Environment, len(existing))
	for _, environment := range existing {
		if environment != nil {
			byName[environment.Name] = environment
		}
	}

	imported := 0
	var warnings []string
	for _, file := range files {
		if _, exists := byName[file.name]; exists {
			warnings = append(warnings, fmt.Sprintf(
				"%s: environment %q already exists for this collection; local environment kept",
				file.filename, file.name,
			))
			continue
		}
		environment := &domain.Environment{CollectionID: collectionID, Name: file.name}
		environment.SetVars(file.vars)
		if err := st.SaveEnvironment(ctx, environment); err != nil {
			return imported, warnings, fmt.Errorf("save environment %q: %w", file.name, err)
		}
		byName[file.name] = environment
		imported++
	}
	return imported, warnings, nil
}

// mergeParsedEnvironmentsIntoGlobal adds variables to Quark's Global
// environment. Existing Global values win, then the first file that defines a
// key; conflicts produce redacted warnings.
func mergeParsedEnvironmentsIntoGlobal(
	ctx context.Context,
	st Store,
	files []parsedEnvironmentFile,
	logger Logger,
) ([]string, error) {
	global, err := st.GetGlobalEnvironment(ctx)
	if err != nil {
		logger.Logf("get global env failed: %v", err)
		return nil, err
	}

	vars, err := global.DecodeVars()
	if err != nil {
		return nil, fmt.Errorf("decode Global: %w", err)
	}
	clone := *global
	sources := make(map[string]string, len(vars))
	for key := range vars {
		sources[key] = globalEnvironmentDisplayName
	}
	changed := false
	var warnings []string
	for _, file := range files {
		keys := make([]string, 0, len(file.vars))
		for key := range file.vars {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := file.vars[key]
			if existing, present := vars[key]; present {
				if existing != value {
					warnings = append(
						warnings,
						fmt.Sprintf(
							"%s: variable %q conflicts with %s; existing value kept",
							file.filename,
							key,
							sources[key],
						),
					)
				}
				continue
			}
			vars[key] = value
			sources[key] = file.filename
			changed = true
		}
	}
	if !changed {
		return warnings, nil
	}
	clone.SetVars(vars)
	if err := st.SaveEnvironment(ctx, &clone); err != nil {
		logger.Logf("save merged global env failed: %v", err)
		return warnings, err
	}
	logger.Logf(
		"merged %d environment file(s) into Global, %d variables total",
		len(files),
		len(vars),
	)
	return warnings, nil
}

// parseEnvironmentsInDir parses all .json files in the environment/ subdirectory.
func parseEnvironmentsInDir(dir string, logger Logger) parsedEnvironmentResult {
	envDir := filepath.Join(dir, "environment")
	entries, err := os.ReadDir(envDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("no environment/ directory")
			return parsedEnvironmentResult{}
		}
		logger.Logf("no environment/ directory: %v", err)
		return parsedEnvironmentResult{
			errors: []string{fmt.Sprintf("read environment directory: %v", err)},
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var result parsedEnvironmentResult
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(envDir, entry.Name())
		f, err := os.Open(path)
		if err != nil {
			errMsg := fmt.Sprintf("%s: open failed: %v", entry.Name(), err)
			logger.Logf("%s", errMsg)
			result.errors = append(result.errors, errMsg)
			continue
		}
		pmEnv, err := postman.ParseEnvironment(f)
		f.Close()
		if err != nil {
			errMsg := fmt.Sprintf("%s: %v", entry.Name(), err)
			logger.Logf("parse environment file failed: %s", entry.Name())
			result.errors = append(result.errors, errMsg)
			continue
		}
		vars, warnings := pmEnv.ToMapWithWarnings()
		for _, warning := range warnings {
			result.warnings = append(result.warnings, fmt.Sprintf("%s: %s", entry.Name(), warning))
		}
		result.files = append(result.files, parsedEnvironmentFile{
			filename: entry.Name(),
			name:     pmEnv.Name,
			scope:    pmEnv.Scope,
			vars:     vars,
		})
	}
	return result
}
