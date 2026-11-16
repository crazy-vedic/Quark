#!/usr/bin/env bash
# Generate shell completion files for quark.
#
# Usage: scripts/gen-completions.sh [output-dir]   (default: completions)
#
# Used by `make generate-completions` (writes the tracked completions/ dir) and
# by the GoReleaser before-hook (writes the gitignored .completions/ dir, so a
# release build never dirties tracked files).
set -euo pipefail

out="${1:-completions}"
mkdir -p "$out"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
bin="$tmp/quark$(go env GOEXE)"

go build -o "$bin" ./cmd/quark
"$bin" completion bash > "$out/quark.bash"
"$bin" completion zsh > "$out/_quark"
"$bin" completion fish > "$out/quark.fish"

echo "Generated completions in $out/"
