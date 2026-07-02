# Quark Makefile
# =============
# HTTP CLI tool — build, test, run, lint.
#
# Pass extra args to any target:
#   make run ARGS="--help"
#   make ut ARGS="-v -run TestFoo"
#   make e2e ARGS="-count=1"
#   make lint ARGS="--fast"

# ---------------------------------------------------------------------------
# Shell
# ---------------------------------------------------------------------------
# Ensure pipelines preserve the test-runner's exit code (tee would swallow it
# under /bin/sh).  bash + pipefail makes "cmd | tee log" exit with cmd's code.
SHELL       := /bin/bash
.SHELLFLAGS := -e -o pipefail -c

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
BINARY      := quark
CMD_DIR     := ./cmd/quark
BIN_DIR     := bin

VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS     := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)

# Extra args passed through to the underlying command.
ARGS        ?=

# Test runner — use gotestsum for nice output + timing if available.
NCPU        := $(shell nproc 2>/dev/null || sysctl -n hw.logicalcpu 2>/dev/null || echo 4)
GOTESTSUM   := $(shell command -v gotestsum 2>/dev/null)
TEST_LOG    := test-output.log

# Helper: run go test via gotestsum (or plain go test if not installed).
# Uses pkgname-and-test-fails so the log contains one package-per-line with
# timing — exactly what the show-slow target parses.
# Usage: $(call go-test,packages,extra-args)
define go-test
	$(if $(GOTESTSUM),\
		gotestsum --format pkgname-and-test-fails -- -race -p $(NCPU) $(2) $(1),\
		go test -race -p $(NCPU) $(2) $(1))
endef

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help: ## Show available targets
	@echo "Quark — HTTP CLI Tool"
	@echo ""
	@echo "Usage: make <target> [ARGS=\"<extra args>\"]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make build              # Build the binary"
	@echo "  make run ARGS=\"--help\" # Run with extra args"
	@echo "  make ut ARGS=\"-v\"      # Run unit tests with verbose"
	@echo "  make e2e                # Run end-to-end tests"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
.PHONY: build
build: ## Build the binary
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD_DIR)
	@echo "Built: $(BIN_DIR)/$(BINARY)"

.PHONY: build-cover
build-cover: ## Build coverage-instrumented binary for e2e CLI tests
	@mkdir -p $(BIN_DIR)
	go build -cover -coverpkg=./... -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY)-cover $(CMD_DIR)
	@echo "Built (with coverage): $(BIN_DIR)/$(BINARY)-cover"

.PHONY: run
run: ## Run the application (pass args via ARGS=...)
	go run $(CMD_DIR) $(ARGS)

# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------
.PHONY: ut
ut: ## Run unit tests (excludes e2e / integration tests)
	$(call go-test,./...,-v -coverpkg=./... $(ARGS)) | tee $(TEST_LOG)

.PHONY: e2e-tui
e2e-tui: ## Run TUI e2e tests (requires e2e build tag)
	$(call go-test,./tests/e2e/tui/...,-v -tags e2e -coverpkg=./... $(ARGS)) | tee $(TEST_LOG)

# e2e-cli: build + run (local dev — always rebuilds the coverage binary)
.PHONY: e2e-cli
e2e-cli: build-cover e2e-cli-run

# e2e-cli-prep: place the coverage binary at the path tests expect.
.PHONY: e2e-cli-prep
e2e-cli-prep: ## Copy bin/quark-cover to bin/quark for e2e tests
	@cp $(BIN_DIR)/$(BINARY)-cover $(BIN_DIR)/$(BINARY)
	@chmod +x $(BIN_DIR)/$(BINARY)

# e2e-cli-run: run tests using the existing coverage binary (CI — assumes bin/quark
# is already a coverage-instrumented build, e.g. from a downloaded artifact).
# Depends on e2e-cli-prep so CI can download quark-cover and run a single target.
.PHONY: e2e-cli-run
e2e-cli-run: e2e-cli-prep ## Run CLI e2e tests using existing coverage binary
	@if [ -d "./tests/e2e/cli" ]; then \
		now_ms() { python3 -c 'import time; print(int(time.time() * 1000))'; }; \
		log_ms() { \
			LABEL="$$1"; START_MS="$$2"; END_MS="$$3"; \
			echo "[e2e-cli-run] $$LABEL: $$((END_MS - START_MS))ms"; \
		}; \
		STEP_START=$$(now_ms); \
		mkdir -p $(CURDIR)/$(BIN_DIR)/cover-cli; \
		STEP_END=$$(now_ms); \
		log_ms "prepare-cover-dir" "$$STEP_START" "$$STEP_END"; \
		set +e; \
		TEST_START=$$(now_ms); \
		GOCOVERDIR=$(CURDIR)/$(BIN_DIR)/cover-cli \
		$(call go-test,./tests/e2e/cli/...,-count=1 -tags e2e $(ARGS)) | tee $(TEST_LOG); \
		TEST_EXIT=$$?; \
		TEST_END=$$(now_ms); \
		set -e; \
		log_ms "go-test-plus-tee" "$$TEST_START" "$$TEST_END"; \
		echo "Converting binary coverage data to text format..."; \
		COV_START=$$(now_ms); \
		go tool covdata textfmt -i $(CURDIR)/$(BIN_DIR)/cover-cli -o $(CURDIR)/$(BIN_DIR)/cover-cli.out; \
		COV_END=$$(now_ms); \
		log_ms "covdata-textfmt" "$$COV_START" "$$COV_END"; \
		TOTAL_END=$$(now_ms); \
		log_ms "total-target" "$$STEP_START" "$$TOTAL_END"; \
		exit $$TEST_EXIT; \
	else \
		echo "No CLI e2e tests found"; \
	fi

.PHONY: e2e
e2e: e2e-tui e2e-cli ## Run all e2e tests (TUI + CLI)

.PHONY: integration
integration: ## Run integration tests (requires integration build tag)
	$(call go-test,./...,-v -tags integration $(ARGS)) | tee $(TEST_LOG)

.PHONY: test
test: ut e2e integration ## Run all tests (unit + e2e + integration)

.PHONY: show-slow
show-slow: ## Show top 10 slowest packages from $(TEST_LOG)
	@echo ""
	@echo "=== Top 10 Slowest Packages ==="
	@awk '!/^===/ && !/^DONE/ { \
		for (i = 1; i <= NF; i++) { \
			f = $$i; \
			if (substr(f,1,1) == "(" && substr(f,length(f),1) == ")") { \
				v = substr(f, 2, length(f)-2); \
				if (index(v, "ms") > 0) { \
					gsub(/ms$$/, "", v); \
					printf "%.6f %s\n", v/1000, $$2; break \
				} else if (index(v, "m") > 0 && index(v, "s") > 0) { \
					split(v, p, "m"); gsub(/s$$/, "", p[2]); \
					printf "%.6f %s\n", p[1]*60 + p[2]+0, $$2; break \
				} else if (index(v, "s") > 0) { \
					gsub(/s$$/, "", v); \
					printf "%.6f %s\n", v+0, $$2; break \
				} \
			} \
		} \
	}' $(TEST_LOG) 2>/dev/null | sort -rn | head -10 | \
	awk 'BEGIN { printf "  %-10s  %s\n  %-10s  %s\n", "DURATION", "PACKAGE", "----------", "-------" } \
	     { printf "  %10.3fs  %s\n", $$1, $$2 }'
	@echo "==================================="
	@echo ""
	@echo "Total packages tested:"
	@awk '/^(ok|FAIL|✓|✗|∅)/ {print $$2}' $(TEST_LOG) 2>/dev/null | sort -u | wc -l | awk '{print "  " $$1}'

# ---------------------------------------------------------------------------
# Code Quality
# ---------------------------------------------------------------------------
.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./... $(ARGS)

.PHONY: test-coverage-report
test-coverage-report: ## Run coverage-report script regression tests
	bash ./scripts/coverage-report_test.sh

.PHONY: lint-ci
lint-ci: ## Run golangci-lint with CI timing instrumentation
	@now_ms() { python3 -c 'import time; print(int(time.time() * 1000))'; }; \
	log_ms() { \
		LABEL="$$1"; START_MS="$$2"; END_MS="$$3"; \
		echo "[lint-ci] $$LABEL: $$((END_MS - START_MS))ms"; \
	}; \
	TOTAL_START=$$(now_ms); \
	VERSION_START=$$(now_ms); \
	golangci-lint version; \
	VERSION_END=$$(now_ms); \
	log_ms "version" "$$VERSION_START" "$$VERSION_END"; \
	ENV_START=$$(now_ms); \
	echo "[lint-ci] GOMODCACHE=$$(go env GOMODCACHE)"; \
	echo "[lint-ci] GOCACHE=$$(go env GOCACHE)"; \
	ENV_END=$$(now_ms); \
	log_ms "go-env" "$$ENV_START" "$$ENV_END"; \
	RUN_START=$$(now_ms); \
	golangci-lint run ./... --color never -v --show-stats $(ARGS); \
	RUN_END=$$(now_ms); \
	log_ms "golangci-lint-run" "$$RUN_START" "$$RUN_END"; \
	TOTAL_END=$$(now_ms); \
	log_ms "total-target" "$$TOTAL_START" "$$TOTAL_END"

.PHONY: vet
vet: ## Run go vet
	go vet ./...

# ---------------------------------------------------------------------------
# Clean / Install
# ---------------------------------------------------------------------------
.PHONY: clean
clean: ## Remove build artifacts and cache
	rm -rf $(BIN_DIR)
	go clean -cache

.PHONY: install
install: build ## Install binary to GOPATH/bin
	go install $(CMD_DIR)
