# CI & Release

## CI Pipeline

Workflow: [`.github/workflows/ci.yml`](.github/workflows/ci.yml)

Triggers: PR `opened` / `reopened` / `synchronize`, and pushes to `main`/`master`. Does **not** run on PR body/title edits.

All jobs run on GitHub-hosted **`ubuntu-latest`** runners.

### Job Order

```mermaid
flowchart LR
  build[build]
  lint[lint]
  packageVal[package-validation]
  testUnit[test-unit]
  testE2eLinux[test-e2e-linux]
  testE2eWindows[test-e2e-windows]
  testE2eMacOS[test-e2e-macos]
  persistBaseline[persist-baseline]
  coverageMaster[coverage-master]
  coverageReport[coverage-report]
  build --> persistBaseline
  lint --> persistBaseline
  testUnit --> persistBaseline
  packageVal --> persistBaseline
  testE2eLinux --> persistBaseline
  persistBaseline --> coverageReport
  coverageMaster --> coverageReport
```

| Job | Platform | Purpose |
|---|---|---|
| `build` | Linux | Compile the binary |
| `lint` | Linux | golangci-lint (`make lint-ci`) |
| `package-validation` | Linux | Validate GoReleaser config and snapshot build |
| `test-unit` | Linux | Unit tests with gotestsum, `-race` |
| `test-e2e-linux` | Linux | TUI + CLI e2e tests with coverage (`-tags e2e`) |
| `test-e2e-windows` | Windows | Native Windows TUI + CLI e2e tests |
| `test-e2e-macos` | macOS | Native macOS TUI + CLI e2e tests |
| `persist-baseline` | Linux | Gate after all test+lint jobs pass; caches coverage for PR comparison |
| `coverage-master` | Linux | Coverage baseline on master |
| `coverage-report` | Linux | Coverage diff reporting via `scripts/coverage-report.sh` |

## Release Validation (PR body)

Workflow: [`.github/workflows/release-validation.yml`](.github/workflows/release-validation.yml)

Triggers: PR `opened` / `reopened` / `synchronize` / `edited` (body/title edits). Uses its own concurrency group so edits do not cancel CI.

| Job | Purpose |
|---|---|
| `validate-release` | Parse `Release: vX.Y.Z` from PR body; validate semver; publish GitHub check |

## Linting

Config: [`.golangci.yaml`](.golangci.yaml)

Key enabled linters:

| Linter | Notable setting |
|---|---|
| `errcheck` | Unchecked errors |
| `gosec` | Security (via golangci-lint) |
| `gocyclo` | Complexity max 40 |
| `revive` | Go style |
| `staticcheck` | Static analysis |
| `lll` | Line length 200 |
| `interfacebloat` | Max 5 methods per interface |
| `gci`, `gofmt`, `goimports`, `golines` | Formatting |

Import grouping: standard → external → `github.com/crazy-vedic/quark`.

Local: `make lint` or `make lint ARGS="--fast"`.

## Release Automation

### Automated (on PR merge)

Workflow: [`.github/workflows/tag-on-merge.yml`](.github/workflows/tag-on-merge.yml)

Trigger: push to `main`/`master`.

**Steps:**
1. Find merged PR for the commit
2. Extract version from PR body line: `Release: vX.Y.Z` (or `X.Y.Z`)
3. **Validate strict +1 semver increment** against latest GitHub release (not just tag)
4. Create annotated tag + push (skip if tag exists)
5. **GoReleaser build:** Cross-compiles 5 targets:
   - darwin amd64/arm64
   - linux amd64/arm64
   - windows amd64
6. Generates packages:
   - Archives (tar.gz): Linux, macOS
   - Binaries (.exe): Windows
   - .deb packages: Unsigned (with signing support via environment variable)
   - .pkg: macOS universal
   - SHA256SUMS file
7. Copies install scripts; publishes GitHub Release via `softprops/action-gh-release@v2`
8. **Triggers distribution workflows:**
   - `publish-apt.yml` — publishes .deb packages to apt repository (gh-pages)
   - `publish-homebrew.yml` — updates Homebrew tap formula

See: [`.goreleaser.yaml`](.goreleaser.yaml) for build targets and archive formats.

**To skip release:** omit the `Release:` line from PR body.

### Distribution Workflows

**APT Repository**

Workflow: [`.github/workflows/publish-apt.yml`](.github/workflows/publish-apt.yml)

Triggers on GitHub `release` event (after `tag-on-merge.yml` publishes release).

Requirements (see [USER-REQUIREMENTS.md](USER-REQUIREMENTS.md)):
- `APT_GPG_KEY_ID` repository variable (optional, for signing)
- `APT_GPG_KEY` secret (optional, base64-encoded GPG secret key)
- `APT_GPG_KEY_PASSPHRASE` (optional, can be secret or environment variable)
- GitHub Pages enabled on `gh-pages` branch

Publishes `.deb` packages to `https://<user>.github.io/<repo>/apt/` for easy Linux installation.

**Homebrew Tap**

Workflow: [`.github/workflows/publish-homebrew.yml`](.github/workflows/publish-homebrew.yml)

Triggers on GitHub `release` event.

Requirements (see [USER-REQUIREMENTS.md](USER-REQUIREMENTS.md)):
- `HOMEBREW_TAP_TOKEN` secret (GitHub PAT with write access to homebrew tap repo)

Auto-publishes formula to `homebrew-<user>/homebrew-quark` tap for macOS installation.

### Manual Release

Workflow: [`.github/workflows/release-manual.yml`](.github/workflows/release-manual.yml)

`workflow_dispatch` to build a release from an existing tag.

## PR Template

[`.github/pull_request_template.md`](.github/pull_request_template.md)

Checklist includes:

- Self-review, style, tests
- README updates if needed
- **Agent doc updates:** "Updated cursorrules, CLAUDE.md, AGENT.md if needed" — use `.agents/` for this repo
- Breaking changes section
- **`Release: vX.Y.Z`** line for automated releases

## Version Injection

Makefile injects build metadata:

```
LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)
```

`VERSION` from `git describe --tags --always --dirty` or `"dev"`.

## Contributing Workflow

1. Branch from `main`
2. Make changes + tests
3. `make test && make lint` locally
4. Open PR with description and checklist
5. Add `Release: vX.Y.Z` if releasing (must be exactly +1 from latest release)
6. Merge → CI runs → tag + release if `Release:` present

## Agent Doc Maintenance

When making changes that affect architecture, CLI, config, or caveats:

1. Update the relevant `.agents/*.md` file(s)
2. Update [INDEX.md](INDEX.md) task router if new workflows emerge
3. Mention in PR checklist

See also: [testing.md](testing.md), [overview.md](overview.md).
