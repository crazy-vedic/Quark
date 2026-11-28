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

**Windows PowerShell (native Windows):**
```powershell
irm https://github.com/crazy-vedic/Quark/releases/latest/download/install.ps1 | iex
```

**Specific version or platform:**
```bash
curl -sL https://raw.githubusercontent.com/crazy-vedic/Quark/master/install.sh | bash -s -- --version v1.0.0 --platform linux-amd64
```

Private repos: set `GITHUB_TOKEN` or `GITHUB_ACCESS_TOKEN` — auth headers forwarded automatically.

### Homebrew (macOS / Linux)

```bash
brew install crazy-vedic/quark/quark
```

Update with:
```bash
brew upgrade quark
```

### apt (Debian / Ubuntu)

Add the Quark repository and install:
```bash
curl -fsSL https://crazy-vedic.github.io/Quark/apt/KEY.pub | sudo tee /usr/share/keyrings/quark-archive-keyring.gpg > /dev/null
echo "deb [signed-by=/usr/share/keyrings/quark-archive-keyring.gpg] https://crazy-vedic.github.io/Quark/apt stable main" | sudo tee /etc/apt/sources.list.d/quark.list
sudo apt update
sudo apt install quark
```

Update with:
```bash
sudo apt upgrade quark
```

### Windows Package Manager (winget)

```bash
winget install Quark.Quark
```

Update with:
```bash
winget upgrade Quark.Quark
```

### npm

```bash
npm install -g @quark-client/quark
```

Or run without installing:
```bash
npx @quark-client/quark
```

Update with:
```bash
npm install -g @quark-client/quark@latest
```

### From source

```bash
git clone https://github.com/crazy-vedic/Quark.git && cd Quark
make build && make install
```

On native Windows, the Makefile requires a Unix-compatible shell. With the Go
toolchain installed, build directly from PowerShell instead:

```powershell
git clone https://github.com/crazy-vedic/Quark.git
cd Quark
go build -o quark.exe ./cmd/quark
```

Run `./quark.exe`, or move it to a directory on your `PATH`.

---

## Quick Start

```bash
quark                         # Launch TUI
quark collection create "API" # Create collection
quark import curl "curl -X POST https://api.example.com/orders" --collection <id> --name "Create Order"
quark run "API/Create Order"  # Execute saved request
quark history "API/Create Order" # List its recorded runs
quark request export-curl "API/Create Order"  # Print it as a curl command
quark collection list         # List all collections
```

---

## Updates

Quark checks for updates automatically and displays a notification in the TUI status bar when a new version is available.

### Manual Update Checks

```bash
quark version                 # Show current version
quark update check            # Check for updates explicitly
```

Update installation depends on your installation method:
- **Homebrew:** `brew upgrade quark`
- **apt:** `sudo apt upgrade quark`
- **winget:** `winget upgrade Quark.Quark`
- **npm:** `npm install -g @quark-client/quark@latest`
- **Direct/Binary:** Re-run the install script or download from GitHub Releases

### Configuration

Auto-checks are enabled by default. To disable or customize:

```bash
# Edit ~/.quark/config.toml
[updates]
check = false                      # Disable update checks
check_interval = "24h"             # How often to check (default 12h)
retry_after_failure = "1h"         # Retry interval on failure (default 1h)

[format]
json_on_save = false               # Disable JSON auto-formatting (default: enabled)
```

JSON formatting preserves the existing indent style (2-space, 4-space, tabs) and is applied when you save a request body that parses as valid JSON. Invalid JSON or non-JSON text is left unchanged.

---

## Features

- **Local SQLite** — `~/.quark/quark.db`, WAL mode, no network
- **Collections & requests** — Organise, sort, enable/disable
- **Import** — cURL and Postman Collection v2.1 from the CLI or TUI
- **Response preview** — Syntax-highlighted JSON, streaming for large bodies
- **Execution history** — Every run of a saved request is kept; history stores the first 1 MiB of each response body and marks larger bodies as truncated
- **Themes** — Terminal-aware dark and light palettes, local custom themes, live preview and reload
- **Fuzzy search** — `/` to search across everything
- **Auto-backup** — Last 10 backups in `~/.quark/backup/`
- **Signal-aware** — SIGINT cancels in-flight, restores terminal
- **Scheduling** — Persist due times with `quark schedule add`; Quark is **not** a background daemon. Runs fire when you invoke `quark schedule run-due` (cron/systemd/CI) or while the TUI is open (in-app timer)

### Import a cURL command in the TUI

Press `I` to open the multiline cURL importer. `Ctrl+V` reads the complete
clipboard, `Ctrl+S` parses it, and `Enter` confirms the preview. The importer
preserves repeated headers, request bodies, JSON, URL-encoded data, and
multipart text fields.

In the request pane, press `x` to copy the selected request as a shell-quoted
cURL command. The command includes its method, headers, authorization, URL, and
body.

`--cert`/`--cert-type P12`, PEM `--cert` plus `--key`, and `--cacert` are saved
as host-scoped TLS configuration. File- or stdin-backed bodies, shell
expansions, redirections, and multiple URLs are rejected rather than executed.

From the TUI sidebar, press `I` to import a Postman Collection v2.1 JSON file
or bulk export directory; `i` opens the cURL importer. Existing collection names
are imported as numbered copies. The same import is available with
`quark import-postman <file-or-directory>`.

Use `quark collection rename <collection> <new-name>` and
`quark collection delete <collection>` to manage collections from scripts.
`quark cert list`, `quark cert set <host> --file <path>`, and
`quark cert delete <host>` manage host-scoped TLS client certificates. For
PKCS#12 files, prefer `--password-env` to keep passwords out of shell history.

`quark run <Collection/Request>` prints the response body after its status
summary. Valid JSON is pretty-printed and syntax-highlighted in a terminal.
Add `--verbose` to print request details and response headers to stderr; this
can include credentials from the saved request.

`quark history <Collection/Request>` lists the runs kept for a request, newest
first. Pass an execution ID (or a unique prefix) to print one run's status,
headers and stored body.

### Text Selection & Editing

Edit URLs, request headers, bodies, and auth fields with familiar keyboard shortcuts:

**Selection:**
- `Shift+Left/Right` — Select text character by character
- `Shift+Home/End` — Select from cursor to line start/end
- `Ctrl+A` — Select all text in the current field (URL, search, headers, body, auth, env, and viewer find fields; the prompt, schedule, Postman path, client-cert, and import-name inputs do not support selection yet)
- Double-click — Select word at cursor in the request URL field only

**Clipboard:**
- `Ctrl+C` — Copy selected text. With nothing selected it does not quit; the status bar shows "Press q to quit". Use `q` to quit the app.
- `Ctrl+X` — Cut selected text
- `Ctrl+V` — Paste (replaces selection if active)

**Navigation:**
- `Ctrl+Left/Right` — Jump to next/previous word
- Arrow keys — Clear selection and move cursor
- `Backspace/Delete` — Remove selected text or character

All keyboard shortcuts match the behavior of standard text editors (VS Code, Postman, browsers). Selection styling provides visual feedback with the theme's selection color.

**Terminal Compatibility:** Most modern terminals (macOS Terminal, iTerm2, GNOME Terminal, Windows Terminal) support these shortcuts. If a shortcut doesn't work, try the alternative or use mouse selection.

#### Multi-line and word selection

- `Shift+Up/Down` — Extend the selection one line up or down in multi-line fields (bodies)
- `Ctrl+Shift+Left/Right` — Extend the selection word by word

#### Pasting a curl command into the URL field

Pasting a curl command into the URL field (`Ctrl+V`, or pressing `Enter` on a field that already holds one) opens the **Replace request with curl command** dialog instead of inserting the text. Multi-line curl commands pasted into the URL field open the same dialog. The dialog shows the pasted command; press `Ctrl+S` to parse it, review the preview, press `Enter` to replace the active request, or `Esc` to cancel.

#### Text editing keys in config

Text-field keys can be remapped in the `[keybindings]` table of `~/.quark/config.toml`. Only values that differ from the defaults below take effect. Example:

```toml
[keybindings]
select_line_up = "shift+up"
```

| Config key | Default | Action |
| --- | --- | --- |
| `select_char_left` / `select_char_right` | `shift+left` / `shift+right` | Extend selection by character |
| `select_word_left` / `select_word_right` | `ctrl+shift+left` / `ctrl+shift+right` | Extend selection by word |
| `select_line_start` / `select_line_end` | `shift+home` / `shift+end` | Extend selection to line start/end |
| `select_line_up` / `select_line_down` | `shift+up` / `shift+down` | Extend selection by line |
| `select_all` | `ctrl+a` | Select all |
| `copy` / `cut` / `paste` | `ctrl+c` / `ctrl+x` / `ctrl+v` | Clipboard actions |
| `char_left` / `char_right` | `left` / `right` | Move cursor by character |
| `word_left` / `word_right` | `ctrl+left` / `ctrl+right` | Move cursor by word |
| `line_start` / `line_end` | `home` / `end` | Move to line start/end |
| `line_up` / `line_down` | `up` / `down` | Move cursor by line |
| `delete_word` | `ctrl+w` | Delete previous word |
| `find_open` | `ctrl+f` | Open find |
| `find_next` / `find_prev` | `enter` / `shift+enter` | Next/previous find match |
| `history_next` / `history_prev` | `shift+pgdown` / `shift+pgup` | Next/previous response history entry |
| `viewer_find` / `viewer_copy` / `viewer_close` | `f` / `c` / `esc` | Response viewer find, copy, close |

---

## Themes

Quark starts with `auto`, which chooses a dark or light palette from the terminal
background while preserving that background. `dark` and `light` paint their own
backgrounds. `transparent` also preserves the terminal background and follows
its contrast.

In the TUI, press `/`, then type the `>` character followed by `theme` to
switch request search into the command palette. Use Up/Down to preview a theme.
Press Enter to save it, or Esc to restore the previous theme. You can also use:

```bash
quark theme list
quark theme set light
quark theme validate
quark --theme dark tui          # one session only
```

For a custom theme, create `<config-dir>/themes/ocean.toml` (the default config
directory is `~/.quark`):

```toml
extends = "dark" # or "light"

[colors]
foreground = "#c0caf5"
background = "#1a1b26" # use "transparent" to inherit the terminal
muted = "#737aa2"
accent = "#7aa2f7"
info = "#7dcfff"
success = "#9ece6a"
warning = "#e0af68"
error = "#f7768e"
active_border = "#ffffff"
match = "#e0af68"
get = "#9ece6a"
post = "#7aa2f7"
put = "#e0af68"
delete = "#f7768e"
syntax = "monokai" # Chroma style for JSON
```

Every color is optional and inherits from `extends`. Colors use six-digit hex
values. The filename is the theme name: run `quark theme validate ocean`, then
select `ocean` in the TUI or use `quark theme set ocean`. Quark checks the active
custom theme file every two seconds and reloads valid edits. An invalid edit
leaves the last working palette in place and shows an error.

The TUI chooses a theme in this order: `--theme`, `QUARK_THEME`, `[ui].theme` in
`config.toml`, then `auto`. `--config` moves both `config.toml` and `themes/` to
the selected directory.

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

**Headers switched off in Postman**

Quark has no per-header on/off switch, so headers that are disabled in Postman are left out of the import and listed as warnings (header name only). Add `--include-disabled` to import them as active headers instead; the import still warns for each one.
```bash
quark import-postman ./MyCollection.postman_collection.json --include-disabled
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
- Environment files found in a bulk export directory. Postman environments are
  preserved by name as selectable environments on each imported root collection;
  explicit Postman globals are merged into Quark's Global environment.

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
- Select the desired imported environment for the root collection with
  `quark env active <collection-id> <environment-name>` before running a request;
  descendants inherit that root selection and can also have their own active environment
- If a request needs cleanup after import, you can edit URL, body, headers, auth, and environments directly in the TUI

### File-backed request values

In the TUI Body or Headers editor, enter `@path/to/file` as the complete body
or header value. Quark reads the file when the request is sent, so the saved
request keeps the reference and picks up later file changes. Files are limited
to 10 MiB; missing or unreadable files fail before any network request is made.

`Host`, `Content-Length`, `Transfer-Encoding`, and `Connection` headers are
applied to their corresponding HTTP request fields. JSON content types
(`application/json` and `*+json`) are validated and formatted when saved or
sent. Other types, including form, text, multipart, and `Content-Encoding`,
remain byte-preserving declarations.

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

**Automated on PR merge** (after one-time setup).

**First-time setup:**
See [USER-REQUIREMENTS.md](USER-REQUIREMENTS.md) for required secrets, variables, and GitHub Pages configuration. Takes ~5 minutes.

**Release workflow:**
1. Open a PR
2. Add `Release: v1.0.0` to the PR body
3. Merge to `master`
4. GitHub Actions creates the tag and publishes:
   - GitHub Releases (tar.gz, .zip, .deb, .pkg archives)
   - apt repository (to `gh-pages`)
   - Homebrew formula (to `homebrew-quark` tap)
   - npm packages (automatically via OIDC)
   - Winget manifest — **manual** (see submission steps in [DISTRIBUTION.md](DISTRIBUTION.md))

To skip: omit the `Release:` line.

**Winget:** All submissions are manual. First submission requires approval from microsoft/winget-pkgs. See [DISTRIBUTION.md](DISTRIBUTION.md) for detailed steps and [WINGET-AGENT-PLAN.md](WINGET-AGENT-PLAN.md) for a guided workflow.

---

## Requirements

- Go 1.26+ (for building from source)
- Terminal with 256-color support

## License

MIT
