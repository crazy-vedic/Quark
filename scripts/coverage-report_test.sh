#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT_PATH="${ROOT_DIR}/scripts/coverage-report.sh"
TEMP_DIRS=()

cleanup_registered_dirs() {
    local dir
    for dir in "${TEMP_DIRS[@]:-}"; do
        [[ -n "$dir" ]] && rm -rf "$dir"
    done
}

register_temp_dir() {
    TEMP_DIRS+=("$1")
}

trap cleanup_registered_dirs EXIT

fail() {
    echo "FAIL: $1" >&2
    exit 1
}

assert_eq() {
    local actual="$1"
    local expected="$2"
    local message="$3"
    if [[ "$actual" != "$expected" ]]; then
        fail "${message}: expected '${expected}', got '${actual}'"
    fi
}

assert_file_contains_exact_line() {
    local file="$1"
    local expected="$2"
    local message="$3"
    if ! grep -Fxq "$expected" "$file"; then
        fail "${message}: missing line '${expected}' in ${file}"
    fi
}

assert_file_not_contains() {
    local file="$1"
    local unexpected="$2"
    local message="$3"
    if grep -Fq "$unexpected" "$file"; then
        fail "${message}: found unexpected '${unexpected}' in ${file}"
    fi
}

setup_repo() {
    local repo_dir="$1"

    git init -b master "$repo_dir" >/dev/null
    git -C "$repo_dir" config user.name "Quark Test"
    git -C "$repo_dir" config user.email "quark@example.com"

    cat > "${repo_dir}/app.go" <<'EOF'
package main

func compute(n int) int {
	value := n + 1
	return value
}
EOF

    cat > "${repo_dir}/app_test.go" <<'EOF'
package main

import "testing"

func TestCompute(t *testing.T) {
	if got := compute(1); got != 2 {
		t.Fatalf("got %d", got)
	}
}
EOF

    git -C "$repo_dir" add app.go app_test.go
    git -C "$repo_dir" commit -m "base" >/dev/null
}

run_code_change() {
    local repo_dir="$1"
    local state_dir="$2"
    local ut_profile="$3"
    local tui_profile="${4:-}"
    local cli_profile="${5:-}"

    if [[ -z "$tui_profile" ]]; then
        tui_profile="${repo_dir}/tui.out"
        : > "$tui_profile"
    fi
    if [[ -z "$cli_profile" ]]; then
        cli_profile="${repo_dir}/cli.out"
        : > "$cli_profile"
    fi

    (
        cd "$repo_dir"
        COVERAGE_REPORT_STATE_DIR="$state_dir" \
        COVERAGE_REPORT_BASE_REF=HEAD \
        PR_UT="$ut_profile" \
        PR_TUI="$tui_profile" \
        PR_CLI="$cli_profile" \
        "${SCRIPT_PATH}" prepare >/dev/null

        COVERAGE_REPORT_STATE_DIR="$state_dir" \
        COVERAGE_REPORT_BASE_REF=HEAD \
        PR_UT="$ut_profile" \
        PR_TUI="$tui_profile" \
        PR_CLI="$cli_profile" \
        "${SCRIPT_PATH}" code-change >/dev/null
    )
}

run_lint_metrics() {
    local state_dir="$1"
    local pr_lint="$2"
    local master_lint="$3"

    COVERAGE_REPORT_STATE_DIR="$state_dir" \
    PR_LINT="$pr_lint" \
    MASTER_LINT="$master_lint" \
    "${SCRIPT_PATH}" lint-metrics >/dev/null
}

cleanup_dirs() {
    local repo_dir="$1"
    local state_dir="$2"
    rm -rf "$repo_dir" "$state_dir"
}

test_excludes_test_file_changes_and_counts_only_changed_prod_lines() {
    local repo_dir
    local state_dir
    repo_dir="$(mktemp -d)"
    state_dir="$(mktemp -d)"
    register_temp_dir "$repo_dir"
    register_temp_dir "$state_dir"

    setup_repo "$repo_dir"

    cat > "${repo_dir}/app.go" <<'EOF'
package main

func compute(n int) int {
	value := n + 2
	return value
}
EOF

    cat > "${repo_dir}/app_test.go" <<'EOF'
package main

import "testing"

func TestCompute(t *testing.T) {
	if got := compute(1); got != 3 {
		t.Fatalf("got %d", got)
	}
}

func TestAnotherThing(t *testing.T) {}
EOF

    cat > "${repo_dir}/ut.out" <<EOF
mode: set
${repo_dir}/app.go:4.1,4.15 1 1
EOF

    run_code_change "$repo_dir" "$state_dir" "${repo_dir}/ut.out"

    assert_file_contains_exact_line "${state_dir}/changed-files.txt" "app.go" \
        "prepare should keep changed non-test production files"
    assert_file_not_contains "${state_dir}/changed-files.txt" "app_test.go" \
        "prepare should exclude changed test files from MCC inputs"

    # shellcheck disable=SC1090
    source "${state_dir}/state.env"
    assert_eq "${TOTAL_CHANGED_LINES}" "1" \
        "MCC denominator should count only changed executable production lines"
    assert_eq "${UT_COVERED}" "1" \
        "covered changed-line count should reflect only changed production lines"
    assert_eq "${UT_CODE_COUNT}" "1/1" \
        "UT code-change metric should be based on changed production lines only"
    cleanup_dirs "$repo_dir" "$state_dir"
}

test_positive_lint_delta_is_signed_for_display() {
    local state_dir
    local pr_lint
    local master_lint
    state_dir="$(mktemp -d)"
    pr_lint="$(mktemp)"
    master_lint="$(mktemp)"
    register_temp_dir "$state_dir"

    cat > "$pr_lint" <<'EOF'
internal/a.go:1:1: first issue
internal/b.go:2:1: second issue
internal/c.go:3:1: third issue
EOF

    cat > "$master_lint" <<'EOF'
internal/a.go:1:1: first issue
EOF

    run_lint_metrics "$state_dir" "$pr_lint" "$master_lint"

    # shellcheck disable=SC1090
    source "${state_dir}/state.env"
    assert_eq "${LINT_DIFF}" "2" \
        "raw lint delta should remain numeric for threshold checks"
    assert_eq "${LINT_DIFF_DISPLAY}" "+2" \
        "positive lint delta should include an explicit plus sign"

    rm -f "$pr_lint" "$master_lint"
    rm -rf "$state_dir"
}

test_mcc_counts_unique_modified_lines_not_coverage_blocks() {
    local repo_dir
    local state_dir
    repo_dir="$(mktemp -d)"
    state_dir="$(mktemp -d)"
    register_temp_dir "$repo_dir"
    register_temp_dir "$state_dir"

    setup_repo "$repo_dir"

    cat > "${repo_dir}/app.go" <<'EOF'
package main

func compute(n int) int {
	value := n + 2
	return value
}
EOF

    cat > "${repo_dir}/ut.out" <<EOF
mode: set
${repo_dir}/app.go:4.1,4.10 1 1
${repo_dir}/app.go:4.11,4.15 1 1
EOF

    run_code_change "$repo_dir" "$state_dir" "${repo_dir}/ut.out"

    # shellcheck disable=SC1090
    source "${state_dir}/state.env"
    assert_eq "${TOTAL_CHANGED_LINES}" "1" \
        "MCC denominator should count a modified source line once"
    assert_eq "${UT_COVERED}" "1" \
        "covered changed-line count should not be multiplied by coverage blocks"
    assert_eq "${UT_CODE_COUNT}" "1/1" \
        "UT code-change metric should use unique modified source lines"
    cleanup_dirs "$repo_dir" "$state_dir"
}

test_comment_only_changes_do_not_inflate_mcc() {
    local repo_dir
    local state_dir
    repo_dir="$(mktemp -d)"
    state_dir="$(mktemp -d)"
    register_temp_dir "$repo_dir"
    register_temp_dir "$state_dir"

    setup_repo "$repo_dir"

    cat > "${repo_dir}/app.go" <<'EOF'
package main

// compute increments the provided number.
func compute(n int) int {
	value := n + 1
	return value
}
EOF

    cat > "${repo_dir}/ut.out" <<EOF
mode: set
${repo_dir}/app.go:4.1,5.14 1 1
EOF

    run_code_change "$repo_dir" "$state_dir" "${repo_dir}/ut.out"

    # shellcheck disable=SC1090
    source "${state_dir}/state.env"
    assert_eq "${TOTAL_CHANGED_LINES}" "0" \
        "comment-only changes should not contribute to MCC"
    assert_eq "${UT_CODE_COUNT}" "0/0" \
        "comment-only changes should not create changed-line coverage debt"
    cleanup_dirs "$repo_dir" "$state_dir"
}

test_renamed_and_e2e_go_files_are_excluded_from_mcc() {
    local repo_dir
    local state_dir
    repo_dir="$(mktemp -d)"
    state_dir="$(mktemp -d)"
    register_temp_dir "$repo_dir"
    register_temp_dir "$state_dir"

    setup_repo "$repo_dir"

    mkdir -p "${repo_dir}/internal/tui" "${repo_dir}/tests/e2e/tui"
    cat > "${repo_dir}/internal/tui/helper.go" <<'EOF'
package tui

func helper() int {
	return 1
}
EOF

    git -C "$repo_dir" add internal/tui/helper.go
    git -C "$repo_dir" commit -m "add helper" >/dev/null

    mv "${repo_dir}/internal/tui/helper.go" "${repo_dir}/tests/e2e/tui/helper.go"
    git -C "$repo_dir" add -A

    cat > "${repo_dir}/ut.out" <<'EOF'
mode: set
EOF

    run_code_change "$repo_dir" "$state_dir" "${repo_dir}/ut.out"

    if grep -q '[^[:space:]]' "${state_dir}/changed-files.txt"; then
        fail "prepare should exclude renamed files moved under tests/e2e from MCC inputs"
    fi

    # shellcheck disable=SC1090
    source "${state_dir}/state.env"
    assert_eq "${TOTAL_CHANGED_LINES}" "0" \
        "renamed files moved under tests/e2e should not contribute to MCC"
    assert_eq "${UT_CODE_COUNT}" "0/0" \
        "renamed files moved under tests/e2e should not create changed-line coverage debt"
    cleanup_dirs "$repo_dir" "$state_dir"
}

test_multiple_uncovered_runs_render_as_separate_blocks() {
    local repo_dir
    local state_dir
    repo_dir="$(mktemp -d)"
    state_dir="$(mktemp -d)"
    register_temp_dir "$repo_dir"
    register_temp_dir "$state_dir"

    setup_repo "$repo_dir"

    cat > "${repo_dir}/app.go" <<'EOF'
package main

func compute(n int) int {
	a := n + 1
	b := a + 1
	c := b + 1
	d := c + 1
	e := d + 1
	f := e + 1
	return f
}
EOF

    # Alternate uncovered (hits 0) and covered (hits 1) blocks over changed lines
    # to produce three distinct uncovered runs: {4}, {6-7}, {9-10}.
    cat > "${repo_dir}/ut.out" <<EOF
mode: set
${repo_dir}/app.go:4.1,4.12 1 0
${repo_dir}/app.go:5.1,5.12 1 1
${repo_dir}/app.go:6.1,7.12 1 0
${repo_dir}/app.go:8.1,8.12 1 1
${repo_dir}/app.go:9.1,10.12 1 0
EOF

    run_code_change "$repo_dir" "$state_dir" "${repo_dir}/ut.out"

    local block_count
    block_count=$(grep -c '^```go' "${state_dir}/ut-uncovered.md" || true)
    assert_eq "$block_count" "3" \
        "three uncovered runs should render as three separate go code blocks"

    assert_file_contains_exact_line "${state_dir}/ut-uncovered.md" "// app.go:4" \
        "first uncovered block should start at line 4"
    assert_file_contains_exact_line "${state_dir}/ut-uncovered.md" "// app.go:6" \
        "second uncovered block should start at line 6"
    assert_file_contains_exact_line "${state_dir}/ut-uncovered.md" "// app.go:9" \
        "third uncovered block should start at line 9"

    cleanup_dirs "$repo_dir" "$state_dir"
}

test_e2e_suites_exclude_cross_surface_files() {
    local repo_dir
    local state_dir
    repo_dir="$(mktemp -d)"
    state_dir="$(mktemp -d)"
    register_temp_dir "$repo_dir"
    register_temp_dir "$state_dir"

    setup_repo "$repo_dir"

    mkdir -p "${repo_dir}/internal/tui" "${repo_dir}/internal/cli" "${repo_dir}/internal/tuitest"
    cat > "${repo_dir}/internal/tui/widget.go" <<'EOF'
package tui

func Widget() int {
	return 1
}
EOF
    cat > "${repo_dir}/internal/cli/command.go" <<'EOF'
package cli

func Command() int {
	return 1
}
EOF
    cat > "${repo_dir}/internal/tuitest/harness.go" <<'EOF'
package tuitest

func Harness() int {
	return 1
}
EOF

    git -C "$repo_dir" add internal/tui/widget.go internal/cli/command.go internal/tuitest/harness.go
    git -C "$repo_dir" commit -m "add tui and cli surfaces" >/dev/null

    cat > "${repo_dir}/internal/tui/widget.go" <<'EOF'
package tui

func Widget() int {
	return 10
}
EOF
    cat > "${repo_dir}/internal/cli/command.go" <<'EOF'
package cli

func Command() int {
	return 20
}
EOF
    cat > "${repo_dir}/internal/tuitest/harness.go" <<'EOF'
package tuitest

func Harness() int {
	return 30
}
EOF

    cat > "${repo_dir}/ut.out" <<EOF
mode: set
${repo_dir}/internal/tui/widget.go:4.1,4.13 1 1
${repo_dir}/internal/cli/command.go:4.1,4.13 1 1
${repo_dir}/internal/tuitest/harness.go:4.1,4.13 1 1
EOF

    cat > "${repo_dir}/tui.out" <<EOF
mode: set
${repo_dir}/internal/tui/widget.go:4.1,4.14 1 1
${repo_dir}/internal/cli/command.go:4.1,4.14 1 0
${repo_dir}/internal/tuitest/harness.go:4.1,4.14 1 1
EOF

    cat > "${repo_dir}/cli.out" <<EOF
mode: set
${repo_dir}/internal/tui/widget.go:4.1,4.14 1 0
${repo_dir}/internal/cli/command.go:4.1,4.14 1 1
${repo_dir}/internal/tuitest/harness.go:4.1,4.14 1 0
EOF

    run_code_change "$repo_dir" "$state_dir" \
        "${repo_dir}/ut.out" \
        "${repo_dir}/tui.out" \
        "${repo_dir}/cli.out"

    # shellcheck disable=SC1090
    source "${state_dir}/state.env"
    assert_eq "${TUI_CODE_COUNT}" "2/2" \
        "E2E TUI MCC should count TUI-scoped changed lines including tuitest"
    assert_eq "${CLI_CODE_COUNT}" "1/1" \
        "E2E CLI MCC should count only CLI-scoped changed lines"

    assert_file_not_contains "${state_dir}/tui-uncovered.md" "internal/cli/command.go" \
        "E2E TUI uncovered files should exclude CLI-only surface"
    assert_file_not_contains "${state_dir}/cli-uncovered.md" "internal/tui/widget.go" \
        "E2E CLI uncovered files should exclude TUI-only surface"
    assert_file_not_contains "${state_dir}/cli-uncovered.md" "internal/tuitest/harness.go" \
        "E2E CLI uncovered files should exclude TUI test harness"

    cleanup_dirs "$repo_dir" "$state_dir"
}

main() {
    test_excludes_test_file_changes_and_counts_only_changed_prod_lines
    test_positive_lint_delta_is_signed_for_display
    test_mcc_counts_unique_modified_lines_not_coverage_blocks
    test_comment_only_changes_do_not_inflate_mcc
    test_renamed_and_e2e_go_files_are_excluded_from_mcc
    test_multiple_uncovered_runs_render_as_separate_blocks
    test_e2e_suites_exclude_cross_surface_files
    echo "coverage-report tests passed"
}

main "$@"
