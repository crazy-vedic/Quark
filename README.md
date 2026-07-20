# Quark

[![CI](https://github.com/crazy-vedic/Quark/actions/workflows/ci.yml/badge.svg)](https://github.com/crazy-vedic/Quark/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.26-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

> **A local-first, keyboard-driven HTTP client.** No cloud. No accounts. No data leaves your machine.

Quark is a terminal-native TUI that keeps everything in a local SQLite database, with a scriptable CLI for automation and CI. You get the ergonomics of a GUI without the surveillance.

---

## Installation

### One-liner

**macOS / Linux / Git Bash:**
```bash
curl -sL https://github.com/crazy-vedic/Quark/releases/latest/download/install.sh | bash
```

**Specific version or platform:**
```bash
curl -sL https://raw.githubusercontent.com/crazy-vedic/Quark/master/install.sh | bash -s -- --version v1.0.0 --platform linux-amd64
```

Private repos: set `GITHUB_TOKEN` or `GITHUB_ACCESS_TOKEN` — auth headers forwarded automatically.

### From source

```bash
git clone https://github.com/crazy-vedic/Quark.git && cd Quark
make build && make install
```

---

## Quick Start

```bash
quark                         # Launch TUI
quark collection create "API" # Create collection
quark import curl "curl -X POST https://api.example.com/orders" --collection <id> --name "Create Order"
quark run "API/Create Order"  # Execute saved request
quark collection list         # List all collections
```

---

## Features

- **Local SQLite** — `~/.quark/quark.db`, WAL mode, no network
- **Collections & requests** — Organise, sort, enable/disable
- **Import** — cURL and Postman Collection v2.1
- **Response preview** — Syntax-highlighted JSON, streaming for large bodies
- **Fuzzy search** — `s` to search across everything
- **Auto-backup** — Last 10 backups in `~/.quark/backup/`
- **Signal-aware** — SIGINT cancels in-flight, restores terminal
- **Scheduling** — Persist due times with `quark schedule add`; Quark is **not** a background daemon. Runs fire when you invoke `quark schedule run-due` (cron/systemd/CI) or while the TUI is open (in-app timer)

---

## Migrate From Postman

Quark can import both a single Postman Collection v2.1 JSON file and a full Postman bulk export directory.

### 1. Export from Postman

You can migrate using either of these:

- **Single collection export**: export a collection as `Collection v2.1` JSON
- **Bulk export directory**: export your workspace/archive so you get a directory containing collection files and, if present, environment files

### 2. Import into Quark

**Single collection JSON**
```bash
quark import-postman ./MyCollection.postman_collection.json
```

**Bulk export directory**
```bash
quark import-postman ./postman-export/
```

**Override the imported collection name**
```bash
quark import-postman ./MyCollection.postman_collection.json --collection-name "Payments API"
```

**Control duplicate handling**
```bash
quark import-postman ./postman-export/ --on-duplicate merge
```

Supported duplicate actions:

- `duplicate` — keep both, with a new unique collection name
- `merge` — merge requests into the existing collection
- `replace` — replace the existing collection
- `skip` — leave the existing collection untouched

### 3. What gets migrated

- Collections and requests
- HTTP method, URL, headers, and body
- Common auth styles that can be mapped into Quark requests
- Environment files found in a bulk export directory

### 4. Recommended migration flow

1. Export one Postman collection or a bulk export directory
2. Run `quark import-postman ...`
3. Open `quark` and inspect the imported collection in the TUI
4. Verify active environments and request auth on a few important requests
5. Run a smoke request with:

```bash
quark run "Collection Name/Request Name"
```

### 5. Notes

- Quark is local-first: imported data is stored in `~/.quark/quark.db`
- If your Postman requests depend on `{{variables}}`, importing the bulk export directory is best because Quark can also import environment files from it
- If a request needs cleanup after import, you can edit URL, body, headers, auth, and environments directly in the TUI

---

## Development

Agent documentation lives in [`.agents/INDEX.md`](.agents/INDEX.md) — start there when working with AI agents.

```bash
make build       # Build
make run         # Run
make ut          # Unit tests (race)
make e2e         # End-to-end
make test        # All tests
make lint        # Lint + format
make clean       # Clean
make install     # Install to GOPATH/bin
```

### CI

Build → lint → unit tests (gotestsum, race) → e2e → security scan.

### Releases

**Automated on PR merge.**

1. Open a PR
2. Add `Release: v1.0.0` to the PR body
3. Merge to `main`
4. GitHub Actions creates the tag and builds the release

To skip: omit the `Release:` line.

---

## Requirements

- Go 1.26+ (for building from source)
- Terminal with 256-color support

## License

MIT
