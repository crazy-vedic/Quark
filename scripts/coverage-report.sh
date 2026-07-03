#!/usr/bin/env bash
# coverage-report.sh — compare PR coverage to master, compute code-change coverage
# per suite (UT, TUI, CLI), and post a summary comment to the PR.
#
# Environment variables expected:
#   PR_UT, PR_TUI, PR_CLI            — coverage profiles from the PR
#   MASTER_UT, MASTER_TUI, MASTER_CLI — coverage profiles from master
#   PR_LINT, MASTER_LINT             — lint outputs for PR and master
#   PR_NUMBER                        — PR number
#   GITHUB_TOKEN                     — GitHub token for posting comments
#   COVERAGE_REPORT_COMMIT_SHA       — optional commit SHA to show in title
#
# Subcommands:
#   prepare            — collect shared inputs (changed files, state dir)
#   coverage-metrics   — compute per-suite total coverage + deltas
#   lint-metrics       — compute PR/master lint counts
#   code-change        — compute changed-line coverage + uncovered dropdowns
#   finalize           — render comment, post it, and enforce pass/fail
#
# With no subcommand, runs the full pipeline above in order.

set -euo pipefail

STATE_DIR="${COVERAGE_REPORT_STATE_DIR:-/tmp/coverage-report-state}"
STATE_ENV="${STATE_DIR}/state.env"
CHANGED_FILES_FILE="${STATE_DIR}/changed-files.txt"
CHANGED_LINES_FILE="${STATE_DIR}/changed-lines.tsv"
UT_UNCOVERED_FILE="${STATE_DIR}/ut-uncovered.md"
TUI_UNCOVERED_FILE="${STATE_DIR}/tui-uncovered.md"
CLI_UNCOVERED_FILE="${STATE_DIR}/cli-uncovered.md"
COMMENT_FILE="${STATE_DIR}/coverage-comment.md"
STATUS_FILE="${STATE_DIR}/coverage-status"
BASE_REF="${COVERAGE_REPORT_BASE_REF:-origin/master}"

mkdir -p "$STATE_DIR"
touch "$STATE_ENV"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

now_ms() {
    python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
}

log_perf() {
    local label="$1"
    local start_ms="$2"
    local end_ms="$3"
    local duration_ms=$((end_ms - start_ms))
    echo "[coverage-report] ${label}: ${duration_ms}ms"
}

load_state() {
    if [[ -f "$STATE_ENV" ]]; then
        # shellcheck disable=SC1090
        source "$STATE_ENV"
    fi

    if [[ -f "$CHANGED_FILES_FILE" ]]; then
        CHANGED_FILES=$(cat "$CHANGED_FILES_FILE")
    else
        CHANGED_FILES=""
    fi
}

save_state_var() {
    local key="$1"
    local value="${2-}"
    local temp_file="${STATE_ENV}.tmp"

    touch "$STATE_ENV"
    grep -v "^${key}=" "$STATE_ENV" > "$temp_file" || true
    printf "%s=%q\n" "$key" "$value" >> "$temp_file"
    mv "$temp_file" "$STATE_ENV"
}

save_state_text() {
    local target="$1"
    local value="${2-}"
    printf "%s" "$value" > "$target"
}

extract_total() {
    local file="$1"
    if [[ ! -f "$file" ]] || [[ ! -s "$file" ]]; then
        echo "0.0"
        return
    fi
    go tool cover -func="$file" | awk '/^total:/{gsub(/%/, "", $NF); print $NF}'
}

round() {
    printf "%.1f" "$1"
}

format_diff() {
    local diff="$1"
    if (( $(echo "$diff >= 0" | bc -l) )); then
        echo "+$(round "$diff")"
    else
        echo "$(round "$diff")"
    fi
}

format_int_diff() {
    local diff="$1"
    if [[ "$diff" -gt 0 ]]; then
        echo "+${diff}"
        return
    fi
    echo "$diff"
}

check_threshold() {
    local current="$1"
    local baseline="$2"
    local threshold="$3"

    local decrease
    decrease=$(echo "$baseline - $current" | bc -l)

    local max_decrease
    max_decrease=$(echo "$baseline * $threshold / 100" | bc -l)

    if (( $(echo "$decrease > $max_decrease" | bc -l) )); then
        echo "❌"
    else
        echo "✅"
    fi
}

# E2E TUI exercises bubbletea directly; CLI e2e runs the binary. Each suite's
# profile uses -coverpkg=./..., so cross-surface files appear as uncovered noise.
file_in_tui_suite_scope() {
    local file="$1"
    case "$file" in
        internal/cli/*|cmd/*) return 1 ;;
        *) return 0 ;;
    esac
}

file_in_cli_suite_scope() {
    local file="$1"
    case "$file" in
        internal/tui/*|internal/tuitest/*) return 1 ;;
        *) return 0 ;;
    esac
}

filter_changed_files_for_scope() {
    local scope="$1"
    local output_file="$2"

    : > "$output_file"
    while IFS= read -r file; do
        [[ -z "$file" ]] && continue
        case "$scope" in
            tui)
                file_in_tui_suite_scope "$file" && printf "%s\n" "$file" >> "$output_file"
                ;;
            cli)
                file_in_cli_suite_scope "$file" && printf "%s\n" "$file" >> "$output_file"
                ;;
            *)
                printf "%s\n" "$file" >> "$output_file"
                ;;
        esac
    done <<< "${CHANGED_FILES:-}"
}

sum_summary_column_for_scope() {
    local summary_file="$1"
    local column="$2"
    local scope="$3"

    if [[ ! -f "$summary_file" ]] || [[ ! -s "$summary_file" ]]; then
        echo "0"
        return
    fi

    awk -F'\t' -v col="$column" -v scope="$scope" '
    function in_scope(path) {
        if (scope == "all") {
            return 1
        }
        if (scope == "tui") {
            return path !~ /^internal\/cli\// && path !~ /^cmd\//
        }
        if (scope == "cli") {
            return path !~ /^internal\/tui\// && path !~ /^internal\/tuitest\//
        }
        return 1
    }
    in_scope($1) {
        sum += $col
    }
    END {
        print sum + 0
    }' "$summary_file"
}

build_effective_changed_files_for_scope() {
    local scope="$1"
    local ut_summary_file="$2"
    local tui_summary_file="$3"
    local cli_summary_file="$4"
    local output_file="$5"

    : > "$output_file"
    while IFS= read -r file; do
        [[ -z "$file" ]] && continue

        case "$scope" in
            tui)
                file_in_tui_suite_scope "$file" || continue
                ;;
            cli)
                file_in_cli_suite_scope "$file" || continue
                ;;
        esac

        local file_total=0
        if [[ -f "$ut_summary_file" ]]; then
            file_total=$(awk -F'\t' -v target="$file" '$1 == target {print $2; exit}' "$ut_summary_file")
        fi
        if [[ -z "$file_total" || "$file_total" == "0" ]] && [[ -f "$tui_summary_file" ]]; then
            file_total=$(awk -F'\t' -v target="$file" '$1 == target {print $2; exit}' "$tui_summary_file")
        fi
        if [[ -z "$file_total" || "$file_total" == "0" ]] && [[ -f "$cli_summary_file" ]]; then
            file_total=$(awk -F'\t' -v target="$file" '$1 == target {print $2; exit}' "$cli_summary_file")
        fi
        if [[ "${file_total:-0}" -gt 0 ]]; then
            printf "%s\n" "$file" >> "$output_file"
        fi
    done <<< "${CHANGED_FILES:-}"
}

sum_scoped_changed_lines() {
    local summary_file="$1"
    local scope="$2"
    local total=0

    while IFS= read -r file; do
        [[ -z "$file" ]] && continue

        case "$scope" in
            tui)
                file_in_tui_suite_scope "$file" || continue
                ;;
            cli)
                file_in_cli_suite_scope "$file" || continue
                ;;
        esac

        local file_total=0
        if [[ -f "$summary_file" ]] && [[ -s "$summary_file" ]]; then
            file_total=$(awk -F'\t' -v target="$file" '$1 == target {print $2; exit}' "$summary_file")
        fi
        total=$((total + ${file_total:-0}))
    done <<< "${CHANGED_FILES:-}"

    echo "$total"
}

build_profile_index() {
    local profile="$1"
    local output_file="$2"

    if [[ ! -f "$profile" ]] || [[ ! -s "$profile" ]]; then
        : > "$output_file"
        return
    fi

    awk -v changed_file="$CHANGED_FILES_FILE" -v changed_lines_file="$CHANGED_LINES_FILE" '
    function remember_uncovered(rel, start_line, end_line, idx) {
        if (uncovered_count[rel] >= 3) {
            return
        }
        uncovered_count[rel]++
        idx = uncovered_count[rel]
        uncovered_start[rel, idx] = start_line
        uncovered_end[rel, idx] = end_line
    }
    BEGIN {
        n = 0
        while ((getline line < changed_file) > 0) {
            if (line != "") {
                changed[++n] = line
            }
        }
        close(changed_file)

        while ((getline cline < changed_lines_file) > 0) {
            if (cline == "") {
                continue
            }
            split(cline, parts, "\t")
            rel = parts[1]
            line_no = parts[2] + 0
            if (rel == "" || line_no <= 0) {
                continue
            }
            if (!((rel SUBSEP line_no) in changed_line)) {
                changed_line[rel, line_no] = 1
                changed_line_count[rel]++
                changed_line_order[rel, changed_line_count[rel]] = line_no
            }
        }
        close(changed_lines_file)
    }
    {
        split($0, parts, " ")
        if (length(parts) < 3) {
            next
        }

        split(parts[1], loc, ":")
        fullpath = loc[1]
        range = loc[2]
        hits = parts[3] + 0

        rel = ""
        for (i = 1; i <= n; i++) {
            cf = changed[i]
            if (length(fullpath) >= length(cf) &&
                substr(fullpath, length(fullpath) - length(cf) + 1) == cf) {
                rel = cf
                break
            }
        }
        if (rel == "") {
            next
        }
        if (!(rel in changed_line_count)) {
            next
        }

        split(range, pair, ",")
        split(pair[1], start_part, ".")
        split(pair[2], end_part, ".")
        start_line = start_part[1] + 0
        end_line = end_part[1] + 0

        for (line_no = start_line; line_no <= end_line; line_no++) {
            if (!((rel SUBSEP line_no) in changed_line)) {
                continue
            }

            coverable_line[rel, line_no] = 1
            if (hits > 0) {
                covered_line[rel, line_no] = 1
            }
        }
    }
    END {
        for (i = 1; i <= n; i++) {
            rel = changed[i]
            if (!(rel in changed_line_count)) {
                continue
            }
            total_count = 0
            covered_count = 0
            open_uncovered = 0

            for (line_idx = 1; line_idx <= changed_line_count[rel]; line_idx++) {
                line_no = changed_line_order[rel, line_idx]
                if (!((rel SUBSEP line_no) in coverable_line)) {
                    continue
                }

                total_count++
                if ((rel SUBSEP line_no) in covered_line) {
                    covered_count++
                    if (open_uncovered) {
                        remember_uncovered(rel, uncovered_run_start, uncovered_run_end)
                        open_uncovered = 0
                    }
                    continue
                }

                if (!open_uncovered) {
                    uncovered_run_start = line_no
                    uncovered_run_end = line_no
                    open_uncovered = 1
                    continue
                }

                if (line_no == uncovered_run_end + 1) {
                    uncovered_run_end = line_no
                    continue
                }

                remember_uncovered(rel, uncovered_run_start, uncovered_run_end)
                uncovered_run_start = line_no
                uncovered_run_end = line_no
            }

            if (open_uncovered) {
                remember_uncovered(rel, uncovered_run_start, uncovered_run_end)
            }

            if (total_count == 0 && covered_count == 0 && !(rel in uncovered_count)) {
                continue
            }

            printf "%s\t%d\t%d\t%d", rel, total_count, covered_count, uncovered_count[rel] + 0
            for (j = 1; j <= uncovered_count[rel]; j++) {
                printf "\t%d:%d", uncovered_start[rel, j], uncovered_end[rel, j]
            }
            printf "\n"
        }
    }' "$profile" > "$output_file"
}

sum_summary_column() {
    local summary_file="$1"
    local column="$2"
    if [[ ! -f "$summary_file" ]] || [[ ! -s "$summary_file" ]]; then
        echo "0"
        return
    fi
    awk -F'\t' -v col="$column" '{sum += $col} END {print sum + 0}' "$summary_file"
}

format_code_change() {
    local covered="$1"
    local total="$2"

    if [[ $total -gt 0 ]]; then
        local pct
        pct=$(echo "scale=1; $covered * 100 / $total" | bc)
        if (( $(echo "$pct >= 50" | bc -l) )); then
            echo "✅ $pct ${covered}/${total}"
        else
            echo "❌ $pct ${covered}/${total}"
        fi
    else
        echo "✅ 0.0 0/0"
    fi
}

generate_uncovered_dropdown_from_summary() {
    local summary_file="$1"
    local suite_name="$2"
    local file_list_file="${3:-$CHANGED_FILES_FILE}"

    if [[ ! -f "$file_list_file" ]] || [[ ! -s "$file_list_file" ]]; then
        return
    fi

    local max_files=10
    local temp_details=""
    local uncovered_files=0
    local file total covered uncov_count range1 range2 range3
    local summary_line

    while IFS= read -r file; do
        [[ -z "$file" ]] && continue

        summary_line=""
        if [[ -f "$summary_file" ]] && [[ -s "$summary_file" ]]; then
            summary_line=$(awk -F'\t' -v target="$file" '$1 == target {print; exit}' "$summary_file")
        fi
        if [[ -z "$summary_line" ]]; then
            continue
        fi

        IFS=$'\t' read -r _ total covered uncov_count range1 range2 range3 <<< "$summary_line"
        if [[ "${uncov_count:-0}" -le 0 ]]; then
            continue
        fi

        local uncovered_blocks=""
        local block_count="${uncov_count:-0}"
        local range
        for range in "${range1:-}" "${range2:-}" "${range3:-}"; do
            [[ -z "$range" ]] && continue
            local start_line="${range%%:*}"
            local end_line="${range##*:}"

            if [[ -f "$file" ]]; then
                local lines_content
                lines_content=$(sed -n "${start_line},${end_line}p" "$file" 2>/dev/null || echo "[unable to read lines]")
                uncovered_blocks="${uncovered_blocks}

\`\`\`go
// ${file}:${start_line}
${lines_content}
\`\`\`"
            else
                uncovered_blocks="${uncovered_blocks}

\`\`\`go
// ${file}:${start_line} [file not found]
\`\`\`"
            fi
        done

        if [[ "$block_count" -gt 0 ]]; then
            temp_details="${temp_details}
  <details>
    <summary>${file} (${block_count} uncovered block(s))</summary>
${uncovered_blocks}
  </details>"
            uncovered_files=$((uncovered_files + 1))
        fi

        if [[ "$uncovered_files" -ge "$max_files" ]]; then
            break
        fi
    done < "$file_list_file"

    if [[ "$uncovered_files" -lt "$max_files" ]]; then
        while IFS= read -r file; do
            [[ -z "$file" ]] && continue
            if [[ -f "$summary_file" ]] && [[ -s "$summary_file" ]] && awk -F'\t' -v target="$file" '$1 == target {found=1; exit} END {exit !found}' "$summary_file"; then
                continue
            fi

            temp_details="${temp_details}
  <details>
    <summary>${file} (0% coverage — not exercised by this suite)</summary>

\`\`\`go
// ${file} [not exercised by this suite]
[no coverage data available for this changed file]
\`\`\`
  </details>"
            uncovered_files=$((uncovered_files + 1))

            if [[ "$uncovered_files" -ge "$max_files" ]]; then
                break
            fi
        done < "$file_list_file"
    fi

    local dropdown=""
    if [[ "$uncovered_files" -gt 0 ]]; then
        local summary_text="${suite_name} — ${uncovered_files} uncovered file(s)"
        if [[ "$uncovered_files" -ge "$max_files" ]]; then
            summary_text="${summary_text} (first ${max_files} shown; see full coverage artifact for rest)"
        fi
        dropdown="<details>
  <summary>${summary_text}</summary>
${temp_details}
</details>"
    fi

    echo "$dropdown"
}

read_text_file() {
    local file="$1"
    if [[ -f "$file" ]]; then
        cat "$file"
    fi
}

resolve_diff_base() {
    if git rev-parse --verify HEAD >/dev/null 2>&1; then
        git merge-base "$BASE_REF" HEAD 2>/dev/null || printf "%s\n" "$BASE_REF"
        return
    fi
    printf "%s\n" "$BASE_REF"
}

prepare() {
    rm -rf "$STATE_DIR"
    mkdir -p "$STATE_DIR"
    : > "$STATE_ENV"

    local diff_base
    diff_base=$(resolve_diff_base)

    local changed_files
    changed_files=$(
        git diff -M --name-only "$diff_base" -- "*.go" \
            | grep -v '^tests/e2e/' \
            | grep -v '_test\.go$' \
            | sort -u || true
    )
    printf "%s\n" "$changed_files" > "$CHANGED_FILES_FILE"

    git diff -M -U0 "$diff_base" -- "*.go" | awk '
    /^diff --git / {
        file = ""
        skip = 0
        next
    }
    /^\+\+\+ b\// {
        file = substr($0, 7)
        skip = (file ~ /^tests\/e2e\// || file ~ /_test\.go$/)
        next
    }
    /^@@ / {
        if (skip || file == "") {
            next
        }
        hunk = $0
        sub(/^@@ -[^ ]+ \+/, "", hunk)
        sub(/ @@.*$/, "", hunk)
        split(hunk, parts, ",")
        start = parts[1] + 0
        count = (length(parts) > 1 ? parts[2] + 0 : 1)
        if (count <= 0) {
            next
        }
        for (i = 0; i < count; i++) {
            printf "%s\t%d\n", file, start + i
        }
    }' > "$CHANGED_LINES_FILE"

    local commit_full commit_short commit_url
    commit_full="${COVERAGE_REPORT_COMMIT_SHA:-}"
    if [[ -z "$commit_full" ]]; then
        commit_full=$(git rev-parse HEAD 2>/dev/null || echo "")
    fi
    commit_short="${commit_full:0:5}"

    commit_url="${COVERAGE_REPORT_COMMIT_URL:-}"
    if [[ -z "$commit_url" && -n "$commit_full" && -n "${GITHUB_SERVER_URL:-}" && -n "${GITHUB_REPOSITORY:-}" ]]; then
        commit_url="${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/commit/${commit_full}"
    fi

    save_state_var "COMMIT_SHA" "$commit_short"
    save_state_var "COMMIT_URL" "$commit_url"
    save_state_var "STATE_DIR" "$STATE_DIR"
    echo "Prepared coverage report state in $STATE_DIR"
}

coverage_metrics() {
    load_state

    local pr_ut_pct pr_tui_pct pr_cli_pct
    local master_ut_pct master_tui_pct master_cli_pct
    local ut_diff tui_diff cli_diff
    local ut_status tui_status cli_status

    pr_ut_pct=$(extract_total "$PR_UT")
    pr_tui_pct=$(extract_total "$PR_TUI")
    pr_cli_pct=$(extract_total "$PR_CLI")

    master_ut_pct=$(extract_total "$MASTER_UT")
    master_tui_pct=$(extract_total "$MASTER_TUI")
    master_cli_pct=$(extract_total "$MASTER_CLI")

    ut_diff=$(format_diff "$(echo "$pr_ut_pct - $master_ut_pct" | bc -l)")
    tui_diff=$(format_diff "$(echo "$pr_tui_pct - $master_tui_pct" | bc -l)")
    cli_diff=$(format_diff "$(echo "$pr_cli_pct - $master_cli_pct" | bc -l)")

    ut_status=$(check_threshold "$pr_ut_pct" "$master_ut_pct" 10)
    tui_status=$(check_threshold "$pr_tui_pct" "$master_tui_pct" 10)

    if [[ "$pr_cli_pct" == "0.0" && "$master_cli_pct" == "0.0" ]]; then
        cli_status="N/A"
    else
        cli_status=$(check_threshold "$pr_cli_pct" "$master_cli_pct" 10)
    fi

    save_state_var "PR_UT_PCT" "$pr_ut_pct"
    save_state_var "PR_TUI_PCT" "$pr_tui_pct"
    save_state_var "PR_CLI_PCT" "$pr_cli_pct"
    save_state_var "MASTER_UT_PCT" "$master_ut_pct"
    save_state_var "MASTER_TUI_PCT" "$master_tui_pct"
    save_state_var "MASTER_CLI_PCT" "$master_cli_pct"
    save_state_var "UT_DIFF" "$ut_diff"
    save_state_var "TUI_DIFF" "$tui_diff"
    save_state_var "CLI_DIFF" "$cli_diff"
    save_state_var "UT_STATUS" "$ut_status"
    save_state_var "TUI_STATUS" "$tui_status"
    save_state_var "CLI_STATUS" "$cli_status"

    echo "Computed suite coverage totals and deltas."
}

count_lint_errors() {
    local file="$1"
    if [[ ! -f "$file" ]]; then
        echo "0"
        return
    fi
    grep -cE '^[^:]+:[0-9]+:[0-9]+:' "$file" 2>/dev/null || true
}

lint_metrics() {
    load_state

    local pr_lint_count master_lint_count lint_diff lint_diff_display
    pr_lint_count=$(count_lint_errors "$PR_LINT")
    master_lint_count=$(count_lint_errors "$MASTER_LINT")
    lint_diff=$((pr_lint_count - master_lint_count))
    lint_diff_display=$(format_int_diff "$lint_diff")

    save_state_var "PR_LINT_COUNT" "$pr_lint_count"
    save_state_var "MASTER_LINT_COUNT" "$master_lint_count"
    save_state_var "LINT_DIFF" "$lint_diff"
    save_state_var "LINT_DIFF_DISPLAY" "$lint_diff_display"

    echo "Computed lint counts."
}

code_change() {
    load_state

    local total_changed_lines lint_threshold lint_status
    local ut_covered tui_covered cli_covered
    local ut_code_change tui_code_change cli_code_change
    local ut_uncovered tui_uncovered cli_uncovered
    local phase_start phase_end
    local work_dir="${STATE_DIR}/code-change"
    local ut_summary_file="${work_dir}/ut-summary.tsv"
    local tui_summary_file="${work_dir}/tui-summary.tsv"
    local cli_summary_file="${work_dir}/cli-summary.tsv"
    local ut_uncovered_file="${work_dir}/ut-uncovered.txt"
    local tui_uncovered_file="${work_dir}/tui-uncovered.txt"
    local cli_uncovered_file="${work_dir}/cli-uncovered.txt"
    local effective_changed_files_file="${work_dir}/effective-changed-files.txt"
    local tui_effective_changed_files_file="${work_dir}/tui-effective-changed-files.txt"
    local cli_effective_changed_files_file="${work_dir}/cli-effective-changed-files.txt"
    local ut_time_file="${work_dir}/ut-index.ms"
    local tui_time_file="${work_dir}/tui-index.ms"
    local cli_time_file="${work_dir}/cli-index.ms"
    local ut_uncovered_time_file="${work_dir}/ut-uncovered.ms"
    local tui_uncovered_time_file="${work_dir}/tui-uncovered.ms"
    local cli_uncovered_time_file="${work_dir}/cli-uncovered.ms"
    local total_lines_time_file="${work_dir}/total-lines.ms"

    local changed_file_count=0
    if [[ -n "${CHANGED_FILES:-}" ]]; then
        changed_file_count=$(printf "%s\n" "$CHANGED_FILES" | sed '/^$/d' | wc -l | tr -d ' ')
    fi
    echo "[coverage-report] code-change: changed_go_files=${changed_file_count}"

    rm -rf "$work_dir"
    mkdir -p "$work_dir"
    : > "$effective_changed_files_file"
    : > "$tui_effective_changed_files_file"
    : > "$cli_effective_changed_files_file"

    (
        phase_start=$(now_ms)
        build_profile_index "$PR_UT" "$ut_summary_file"
        phase_end=$(now_ms)
        echo $((phase_end - phase_start)) > "$ut_time_file"
    ) &
    local ut_covered_pid=$!

    (
        phase_start=$(now_ms)
        build_profile_index "$PR_TUI" "$tui_summary_file"
        phase_end=$(now_ms)
        echo $((phase_end - phase_start)) > "$tui_time_file"
    ) &
    local tui_covered_pid=$!

    (
        phase_start=$(now_ms)
        build_profile_index "$PR_CLI" "$cli_summary_file"
        phase_end=$(now_ms)
        echo $((phase_end - phase_start)) > "$cli_time_file"
    ) &
    local cli_covered_pid=$!

    wait "$ut_covered_pid"
    wait "$tui_covered_pid"
    wait "$cli_covered_pid"

    phase_start=$(now_ms)
    total_changed_lines=0
    while IFS= read -r file; do
        [[ -z "$file" ]] && continue
        local file_total=0
        if [[ -f "$ut_summary_file" ]]; then
            file_total=$(awk -F'\t' -v target="$file" '$1 == target {print $2; exit}' "$ut_summary_file")
        fi
        if [[ -z "$file_total" || "$file_total" == "0" ]] && [[ -f "$tui_summary_file" ]]; then
            file_total=$(awk -F'\t' -v target="$file" '$1 == target {print $2; exit}' "$tui_summary_file")
        fi
        if [[ -z "$file_total" || "$file_total" == "0" ]] && [[ -f "$cli_summary_file" ]]; then
            file_total=$(awk -F'\t' -v target="$file" '$1 == target {print $2; exit}' "$cli_summary_file")
        fi
        if [[ "${file_total:-0}" -gt 0 ]]; then
            printf "%s\n" "$file" >> "$effective_changed_files_file"
        fi
        total_changed_lines=$((total_changed_lines + ${file_total:-0}))
    done <<< "$CHANGED_FILES"

    build_effective_changed_files_for_scope "tui" "$ut_summary_file" "$tui_summary_file" "$cli_summary_file" "$tui_effective_changed_files_file"
    build_effective_changed_files_for_scope "cli" "$ut_summary_file" "$tui_summary_file" "$cli_summary_file" "$cli_effective_changed_files_file"
    phase_end=$(now_ms)
    echo $((phase_end - phase_start)) > "$total_lines_time_file"

    ut_covered=$(sum_summary_column "$ut_summary_file" 3)
    tui_covered=$(sum_summary_column_for_scope "$tui_summary_file" 3 "tui")
    cli_covered=$(sum_summary_column_for_scope "$cli_summary_file" 3 "cli")

    local tui_total_changed_lines cli_total_changed_lines
    tui_total_changed_lines=$(sum_scoped_changed_lines "$tui_summary_file" "tui")
    cli_total_changed_lines=$(sum_scoped_changed_lines "$cli_summary_file" "cli")

    phase_start=$(now_ms)
    generate_uncovered_dropdown_from_summary "$ut_summary_file" "UT" "$effective_changed_files_file" > "$ut_uncovered_file"
    phase_end=$(now_ms)
    echo $((phase_end - phase_start)) > "$ut_uncovered_time_file"

    phase_start=$(now_ms)
    generate_uncovered_dropdown_from_summary "$tui_summary_file" "E2E TUI" "$tui_effective_changed_files_file" > "$tui_uncovered_file"
    phase_end=$(now_ms)
    echo $((phase_end - phase_start)) > "$tui_uncovered_time_file"

    phase_start=$(now_ms)
    generate_uncovered_dropdown_from_summary "$cli_summary_file" "E2E CLI" "$cli_effective_changed_files_file" > "$cli_uncovered_file"
    phase_end=$(now_ms)
    echo $((phase_end - phase_start)) > "$cli_uncovered_time_file"

    ut_uncovered=$(cat "$ut_uncovered_file")
    tui_uncovered=$(cat "$tui_uncovered_file")
    cli_uncovered=$(cat "$cli_uncovered_file")

    echo "[coverage-report] code-change.compute_total_lines: $(cat "$total_lines_time_file")ms"
    echo "[coverage-report] code-change.compute_covered_lines.ut: $(cat "$ut_time_file")ms"
    echo "[coverage-report] code-change.compute_covered_lines.tui: $(cat "$tui_time_file")ms"
    echo "[coverage-report] code-change.compute_covered_lines.cli: $(cat "$cli_time_file")ms"
    echo "[coverage-report] code-change.generate_uncovered_dropdown.ut: $(cat "$ut_uncovered_time_file")ms"
    echo "[coverage-report] code-change.generate_uncovered_dropdown.tui: $(cat "$tui_uncovered_time_file")ms"
    echo "[coverage-report] code-change.generate_uncovered_dropdown.cli: $(cat "$cli_uncovered_time_file")ms"

    lint_threshold=$((total_changed_lines / 100))
    if [[ "$lint_threshold" -lt 1 ]]; then
        lint_threshold=1
    fi

    if [[ "${LINT_DIFF:-0}" -gt "$lint_threshold" ]]; then
        lint_status="❌"
    else
        lint_status="✅"
    fi

    ut_code_change=$(format_code_change "$ut_covered" "$total_changed_lines")
    tui_code_change=$(format_code_change "$tui_covered" "$tui_total_changed_lines")
    cli_code_change=$(format_code_change "$cli_covered" "$cli_total_changed_lines")

    echo "[coverage-report] code-change: total_changed_lines=${total_changed_lines} tui_changed_lines=${tui_total_changed_lines} cli_changed_lines=${cli_total_changed_lines} lint_threshold=${lint_threshold}"

    save_state_var "TOTAL_CHANGED_LINES" "$total_changed_lines"
    save_state_var "MCC" "$total_changed_lines"
    save_state_var "LINT_THRESHOLD" "$lint_threshold"
    save_state_var "LINT_STATUS" "$lint_status"
    save_state_var "UT_COVERED" "$ut_covered"
    save_state_var "TUI_COVERED" "$tui_covered"
    save_state_var "CLI_COVERED" "$cli_covered"
    save_state_var "UT_CODE_CHANGE" "$ut_code_change"
    save_state_var "TUI_CODE_CHANGE" "$tui_code_change"
    save_state_var "CLI_CODE_CHANGE" "$cli_code_change"
    save_state_var "UT_CODE_STATUS" "$(echo "$ut_code_change" | awk '{print $1}')"
    save_state_var "UT_CODE_PCT" "$(echo "$ut_code_change" | awk '{print $2}')"
    save_state_var "UT_CODE_COUNT" "$(echo "$ut_code_change" | awk '{print $3}')"
    save_state_var "TUI_CODE_STATUS" "$(echo "$tui_code_change" | awk '{print $1}')"
    save_state_var "TUI_CODE_PCT" "$(echo "$tui_code_change" | awk '{print $2}')"
    save_state_var "TUI_CODE_COUNT" "$(echo "$tui_code_change" | awk '{print $3}')"
    save_state_var "CLI_CODE_STATUS" "$(echo "$cli_code_change" | awk '{print $1}')"
    save_state_var "CLI_CODE_PCT" "$(echo "$cli_code_change" | awk '{print $2}')"
    save_state_var "CLI_CODE_COUNT" "$(echo "$cli_code_change" | awk '{print $3}')"

    save_state_text "$UT_UNCOVERED_FILE" "$ut_uncovered"
    save_state_text "$TUI_UNCOVERED_FILE" "$tui_uncovered"
    save_state_text "$CLI_UNCOVERED_FILE" "$cli_uncovered"

    echo "Computed code-change coverage and uncovered-file details."
}

finalize() {
    load_state

    local ut_uncovered tui_uncovered cli_uncovered
    local comment

    ut_uncovered=$(read_text_file "$UT_UNCOVERED_FILE")
    tui_uncovered=$(read_text_file "$TUI_UNCOVERED_FILE")
    cli_uncovered=$(read_text_file "$CLI_UNCOVERED_FILE")

    local title="### Coverage Report"
    if [[ -n "${COMMIT_SHA:-}" ]]; then
        if [[ -n "${COMMIT_URL:-}" ]]; then
            title="${title} - [${COMMIT_SHA}](${COMMIT_URL})"
        else
            title="${title} - \`${COMMIT_SHA}\`"
        fi
    fi

    comment=$(cat <<EOF
${title}

| Check | Value | Delta | MCC |
|-------|-------|-----------|-----|
| Unit Tests | ${PR_UT_PCT}% | ${UT_DIFF}% ${UT_STATUS} | ${UT_CODE_COUNT} (${UT_CODE_PCT}%) ${UT_CODE_STATUS} |
| E2E TUI | ${PR_TUI_PCT}% | ${TUI_DIFF}% ${TUI_STATUS} | ${TUI_CODE_COUNT} (${TUI_CODE_PCT}%) ${TUI_CODE_STATUS} |
| E2E CLI | ${PR_CLI_PCT}% | ${CLI_DIFF}% ${CLI_STATUS} | ${CLI_CODE_COUNT} (${CLI_CODE_PCT}%) ${CLI_CODE_STATUS} |
| Lint | ${PR_LINT_COUNT} | ${LINT_DIFF_DISPLAY:-${LINT_DIFF}}/${LINT_THRESHOLD} ${LINT_STATUS} | - |

**Thresholds:**
- Coverage decrease must not exceed 10% of the previous coverage.
- Code change coverage must be ≥ 50%.
- Lint error increase must not exceed ${LINT_THRESHOLD}.

### Uncovered Files

${ut_uncovered}

${tui_uncovered}

${cli_uncovered}
EOF
)

    printf "%s\n" "$comment" > "$COMMENT_FILE"

    if [[ -n "${PR_NUMBER:-}" && -n "${GITHUB_TOKEN:-}" ]]; then
        gh pr comment "$PR_NUMBER" --body-file "$COMMENT_FILE"
    else
        echo "PR_NUMBER or GITHUB_TOKEN not set; printing comment to stdout:"
        cat "$COMMENT_FILE"
    fi

    if [[ "${UT_STATUS:-}" == "❌" || "${TUI_STATUS:-}" == "❌" || "${CLI_STATUS:-}" == "❌" || "${LINT_STATUS:-}" == "❌" || \
          "${UT_CODE_STATUS:-}" == "❌" || "${TUI_CODE_STATUS:-}" == "❌" || "${CLI_CODE_STATUS:-}" == "❌" ]]; then
        {
            echo "Coverage check failed."
            echo "UT_STATUS=${UT_STATUS:-} TUI_STATUS=${TUI_STATUS:-} CLI_STATUS=${CLI_STATUS:-} LINT_STATUS=${LINT_STATUS:-}"
        } | tee "$STATUS_FILE"
        exit 1
    fi

    {
        echo "All coverage checks passed."
        echo "UT_STATUS=${UT_STATUS:-} TUI_STATUS=${TUI_STATUS:-} CLI_STATUS=${CLI_STATUS:-}"
    } | tee "$STATUS_FILE"
}

run_all() {
    prepare
    coverage_metrics
    lint_metrics
    code_change
    finalize
}

main() {
    local command="${1:-all}"
    case "$command" in
        all)
            run_all
            ;;
        prepare)
            prepare
            ;;
        coverage-metrics)
            coverage_metrics
            ;;
        lint-metrics)
            lint_metrics
            ;;
        code-change)
            code_change
            ;;
        finalize)
            finalize
            ;;
        *)
            echo "Unknown command: $command" >&2
            echo "Usage: $0 [prepare|coverage-metrics|lint-metrics|code-change|finalize]" >&2
            exit 2
            ;;
    esac
}

main "$@"
