# Imports

Quark supports importing requests from cURL commands and Postman Collection v2.1 exports.

## cURL Import (`internal/curl/`)

### CLI Usage

```
quark import curl <curl-command> [--collection <id>] [--name <name>]
```

Implemented in `internal/cli/importcmd.go`, parsing in `internal/curl/`.

### Flow

1. Join args into single curl command string
2. `curl.Importer.Parse(reader)` → `ImportResult`
3. Print method, URL, security level, warnings
4. If `--collection` and `--name` provided: save as `domain.Request`
5. Otherwise: dry run (print only)

### ImportResult (`curl/result.go`)

```go
type ImportResult struct {
    Method   string
    URL      string
    Headers  map[string]string
    Body     string
    Security SecurityLevel  // safe, caution, dangerous
    Warnings []string       // sorted
}
```

### Parser Limits (`curl/parser.go`)

Warnings generated for unsupported or risky patterns:

- Shell variable expansion (`$VAR`, `$(cmd)`)
- `@filename` body references (not read from disk)
- Unsupported curl flags (silently skipped or warned)

Does not execute shell — parses the command string literally.

### TUI Import

TUI import mode accepts pasted cURL via `CurlImporter.Parse` interface. Same parser as CLI.

## Postman Import (`internal/postman/`)

### CLI Usage

```
quark import-postman <collection.json|directory> \
  [--collection-name <name>] \
  [--on-duplicate replace|duplicate|merge|skip]
```

Implemented in `internal/cli/import_postman.go`, parsing in `internal/postman/`.

### Single File

Import one Postman Collection v2.1 JSON:

```bash
quark import-postman ./MyCollection.postman_collection.json
```

### Bulk Directory

Import a Postman bulk export directory containing:

- Collection JSON files (`*.postman_collection.json`)
- Environment files (`*.postman_environment.json`) if present

Best for migrating workspaces with `{{variables}}` — environments are imported alongside collections.

### Duplicate Handling

`--on-duplicate` flag (default: `duplicate`):

| Action | Behavior |
|---|---|
| `replace` | Replace existing collection contents |
| `duplicate` | Create new collection with suffixed name |
| `merge` | Merge requests into existing collection |
| `skip` | Skip if collection name exists |

Interactive TTY may prompt when flag not set and duplicate detected.

### Mapping (`postman/mapper.go`)

Maps Postman collection items to Quark requests:

- Methods, URLs, headers, bodies
- Auth types (with warnings for unsupported modes)
- Folder structure → flat request list with names

**Limitations** (warnings emitted):

- Unsupported Postman auth types (OAuth1, OAuth2, AWS SigV4, etc.)
- Non-raw body modes (formdata, urlencoded partially supported)
- Pre-request/test scripts (not executed — Quark has its own hook system)
- Postman variables in URLs preserved as `{{VAR}}` for Quark env interpolation

### Environment Import (`postman/environment.go`)

Bulk export directories may include `.postman_environment.json` files. Mapped to Quark `Environment` entities with variable key→value pairs.

### Transactions

Bulk and multi-file imports use `store.BeginTransaction` for atomicity — all-or-nothing per import batch.

### Security Classification

Postman importer assigns security levels (similar to cURL) based on URL scheme and auth presence. Displayed in import summary.

## Shared Patterns

Both importers:

- Return sorted `Warnings []string`
- Produce data compatible with `domain.Request` / `domain.Collection`
- Do not execute HTTP during import

## Post-Migration Workflow

After import:

1. Verify requests in TUI or `quark request list`
2. Set up environments: `quark env set` or TUI env mode
3. Test with `quark run "Collection/Request"`
4. Edit auth/headers/body in TUI as needed

See also: [cli-commands.md](cli-commands.md), [domain-and-store.md](domain-and-store.md), [http-execution.md](http-execution.md).
