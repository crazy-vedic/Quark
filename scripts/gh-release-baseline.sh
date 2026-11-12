#!/bin/bash
# gh-release-baseline.sh
# Upload/download baseline artifacts (coverage + lint) to/from GitHub Releases
# Uses tree-SHA as content-addressable key for cross-event reuse
#
# Usage:
#   ./scripts/gh-release-baseline.sh upload <tree-sha> <baseline-dir>
#   ./scripts/gh-release-baseline.sh download <tree-sha> <output-dir>

set -o pipefail

readonly REPO="crazy-vedic/Quark"
readonly RELEASE_PREFIX="tree-cache-"

# Exit codes
readonly SUCCESS=0
readonly FAILURE=1
readonly NOT_FOUND=1

log_info() {
  echo "ℹ️  $*" >&2
}

log_success() {
  echo "✓ $*" >&2
}

log_error() {
  echo "✗ $*" >&2
}

validate_tree_sha() {
  local sha="$1"
  if [[ ! $sha =~ ^[0-9a-f]{40}$ ]]; then
    log_error "Invalid tree-SHA format: $sha (expected 40-char hex)"
    return $FAILURE
  fi
  return $SUCCESS
}

release_exists() {
  local tree_sha="$1"
  local tag="${RELEASE_PREFIX}${tree_sha}"

  if gh api repos/"$REPO"/releases/tags/"$tag" >/dev/null 2>&1; then
    return $SUCCESS
  fi
  return $FAILURE
}

upload_baseline() {
  local tree_sha="$1"
  local baseline_dir="$2"
  local tag="${RELEASE_PREFIX}${tree_sha}"

  # Validate inputs
  if ! validate_tree_sha "$tree_sha"; then
    return $FAILURE
  fi

  if [[ ! -d "$baseline_dir" ]]; then
    log_error "Baseline directory not found: $baseline_dir"
    return $FAILURE
  fi

  # Check if all required files exist
  local required_files=("ut.out" "tui.out" "lint-output.txt")
  for file in "${required_files[@]}"; do
    if [[ ! -f "$baseline_dir/$file" ]]; then
      log_error "Missing required file: $baseline_dir/$file"
      return $FAILURE
    fi
  done

  log_info "Uploading baseline for tree-SHA $tree_sha"

  # Create release if it doesn't exist
  if ! release_exists "$tree_sha"; then
    log_info "Creating draft release: $tag"
    if ! gh release create "$tag" \
      --title "Tree-SHA Cache: ${tree_sha:0:8}" \
      --notes "Baseline cache for tree-SHA $tree_sha" \
      --draft \
      --repo "$REPO" 2>/dev/null; then
      log_error "Failed to create release: $tag"
      return $FAILURE
    fi
  else
    log_info "Release already exists: $tag"
  fi

  # Upload files with --clobber to replace if they exist
  local files_to_upload=(
    "$baseline_dir/ut.out"
    "$baseline_dir/tui.out"
    "$baseline_dir/lint-output.txt"
  )

  # Only add CLI coverage if it exists
  if [[ -f "$baseline_dir/bin/cover-cli.out" ]]; then
    files_to_upload+=("$baseline_dir/bin/cover-cli.out")
  fi

  if ! gh release upload "$tag" \
    "${files_to_upload[@]}" \
    --repo "$REPO" \
    --clobber 2>/dev/null; then
    log_error "Failed to upload baseline files to release: $tag"
    return $FAILURE
  fi

  log_success "Baseline uploaded to release: $tag"
  return $SUCCESS
}

download_baseline() {
  local tree_sha="$1"
  local output_dir="$2"
  local tag="${RELEASE_PREFIX}${tree_sha}"

  # Validate inputs
  if ! validate_tree_sha "$tree_sha"; then
    return $FAILURE
  fi

  # Create output directory
  if ! mkdir -p "$output_dir"; then
    log_error "Failed to create output directory: $output_dir"
    return $FAILURE
  fi

  # Check if release exists
  if ! release_exists "$tree_sha"; then
    log_info "Release not found: $tag (expected on cache-miss)"
    return $NOT_FOUND
  fi

  log_info "Downloading baseline from release: $tag"

  # Download all files from release
  if ! gh release download "$tag" \
    --repo "$REPO" \
    --dir "$output_dir" 2>/dev/null; then
    log_error "Failed to download baseline from release: $tag"
    return $FAILURE
  fi

  # Verify required files were downloaded
  local required_files=("ut.out" "tui.out" "lint-output.txt")
  for file in "${required_files[@]}"; do
    if [[ ! -f "$output_dir/$file" ]]; then
      log_error "Downloaded release missing required file: $file"
      return $FAILURE
    fi
  done

  # Verify CLI coverage location (might be at root or in bin/)
  if [[ -f "$output_dir/cover-cli.out" ]]; then
    mkdir -p "$output_dir/bin"
    mv "$output_dir/cover-cli.out" "$output_dir/bin/cover-cli.out"
  fi

  log_success "Baseline downloaded from release: $tag"
  return $SUCCESS
}

main() {
  if [[ $# -lt 2 ]]; then
    echo "Usage: $0 {upload|download} <tree-sha> <dir>" >&2
    echo "" >&2
    echo "Commands:" >&2
    echo "  upload <tree-sha> <baseline-dir>  - Upload baseline to GitHub Releases" >&2
    echo "  download <tree-sha> <output-dir>  - Download baseline from GitHub Releases" >&2
    exit $FAILURE
  fi

  local command="$1"
  local tree_sha="$2"
  local dir="$3"

  case "$command" in
    upload)
      upload_baseline "$tree_sha" "$dir"
      exit $?
      ;;
    download)
      download_baseline "$tree_sha" "$dir"
      exit $?
      ;;
    *)
      log_error "Unknown command: $command"
      exit $FAILURE
      ;;
  esac
}

main "$@"
