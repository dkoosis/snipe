# Snipe Makefile
#
# Primary: scan check audit report deploy doctor
#   scan   — changed pkgs only (fast inner loop)
#   check  — full repo: vet + lint + test + build
#   audit  — everything: +race +blackbox +eval +vuln
# Run `make help` for full target list.

.DEFAULT_GOAL := check

# Strict shell for recipes: fail on first error, undefined var, or pipe failure.
# REPORT_CMD opts out via `set +e;` so it can keep emitting output past
# tool failures.
SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

# ── Shared sandbox (go-sandbox) ──
include .sandbox/lib/Makefile.doctor.mk
include .sandbox/lib/Makefile.cross.mk

.PHONY: help scan check audit deploy report report-human \
        vet lint test race blackbox vuln pack-drift \
        install clean \
        baseline bench eval eval-setup

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/dkoosis/snipe/cmd.Version=$(VERSION) -X github.com/dkoosis/snipe/cmd.GitCommit=$(COMMIT)

# Report stream — fo dashboard format. `set +e` opts out of the recipe-wide
# -euo pipefail so report MUST run every tool and emit output even if one
# fails. The outer `|| true` on report targets keeps make exit-0 regardless.
REPORT_CMD = set +e; \
	echo '--- tool:build format:sarif ---'; \
	go build ./... 2>&1 | fo wrap diag --tool build --level error; echo; \
	echo '--- tool:vet format:sarif ---'; \
	go vet ./... 2>&1 | fo wrap diag --tool vet --level error; echo; \
	echo '--- tool:lint format:sarif ---'; \
	golangci-lint run ./... 2>&1 | fo wrap diag --tool golangci-lint; echo; \
	echo '--- tool:test format:testjson ---'; \
	go test -json -cover -count=1 ./... 2>&1; echo; \
	echo '--- tool:blackbox format:testjson ---'; \
	go test -json -tags=blackbox -count=1 ./test/blackbox/... 2>&1; echo

## ---------------------------------------------------------------------
## Primary
## ---------------------------------------------------------------------

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^## [^-]/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 4) } \
		/^[a-zA-Z0-9_-]+:.*?## / { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

check: vet lint test pack-drift ## Full repo: vet + lint + test + drift + build
	@go build ./...
	@echo "=== check pass ==="

audit: check race blackbox eval vuln ## Exhaustive: +race +blackbox +eval +vuln
	@echo "=== audit pass ==="

deploy: install ## Build, install, and verify
	@echo "=== deployed ($$(snipe version 2>/dev/null || echo unknown)) ==="

report: ## Structured QA output for agents/tools (always exits 0)
	@( $(REPORT_CMD) ) | fo --format llm --state-file .fo/report.json || true

report-human: ## Same as report, rendered for humans (always exits 0)
	@( $(REPORT_CMD) ) | fo --format human --state-file .fo/report.json || true

## doctor target provided by .sandbox/lib/Makefile.doctor.mk
## cross / cross-amd64 / cross-arm64 targets provided by .sandbox/lib/Makefile.cross.mk

## ---------------------------------------------------------------------
## Checks
## ---------------------------------------------------------------------

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (full)
	golangci-lint run ./...

test: ## Run tests with coverage (fo-rendered)
	go test -json -count=1 -cover ./... | fo --stream=false --state-file .fo/test.json

race: ## Run tests with race detector (slow, fo-rendered)
	go test -json -race -timeout=5m -count=1 ./... | fo --stream=false --state-file .fo/race.json

blackbox: ## Run blackbox integration tests (fo-rendered)
	go test -json -tags=blackbox -count=1 ./test/blackbox/... | fo --stream=false --state-file .fo/blackbox.json

vuln: ## Scan for known vulnerabilities
	govulncheck ./...

# bugclasses pack (ccp-sbp): fail if .golangci-rules/bugclasses.go has drifted
# from the upstream cc-plugins pack. Network-soft — an unreachable upstream
# warns and passes, so this never breaks an offline/private-repo build.
pack-drift: ## Check bugclasses pack for drift from upstream
	@scripts/check-pack-drift.sh .golangci-rules/bugclasses.go

## ---------------------------------------------------------------------
## Build
## ---------------------------------------------------------------------

install: ## Build and install snipe to $GOPATH/bin
	go install -ldflags '$(LDFLAGS)' .

clean: ## Remove build artifacts
	rm -rf .bin bin .snipe .sandbox/bin/linux-amd64 .sandbox/bin/linux-arm64 .sandbox/cache

## ---------------------------------------------------------------------
## Metrics & Eval
## ---------------------------------------------------------------------

baseline: ## Capture performance/quality metrics
	SNIPE_BASELINE=1 go test -v -run TestCaptureBaseline ./test/bench/

bench: ## Run Go benchmarks
	go test -bench=. -benchmem ./test/bench/

eval-setup: ## Clone and index benchmark repos
	@go install . && \
	mkdir -p .eval-repos && \
	for repo in "chi:https://github.com/go-chi/chi" "cobra:https://github.com/spf13/cobra" "bbolt:https://github.com/etcd-io/bbolt" "fzf:https://github.com/junegunn/fzf"; do \
		name=$${repo%%:*}; url=$${repo#*:}; \
		if [ -d ".eval-repos/$$name" ]; then \
			echo "$$name: already cloned"; \
		else \
			echo "$$name: cloning..."; \
			git clone --depth=1 "$$url" ".eval-repos/$$name"; \
		fi; \
		echo "$$name: indexing..."; \
		snipe index "$$(cd .eval-repos/$$name && pwd)" --enrich=false --embed-mode=off; \
	done

eval: ## Run localization benchmark
	go test -v -tags=eval -run TestEval -timeout=10m ./test/eval/

## ---------------------------------------------------------------------
## Utilities
## ---------------------------------------------------------------------

scan: ## Vet + lint + test changed packages only (fast inner loop)
	@PKGS=$$( { git diff --name-only HEAD -- '*.go'; git ls-files --others --exclude-standard -- '*.go'; } \
		| xargs dirname 2>/dev/null | sort -u | sed 's|^|./|' | grep -v '^\./$$'); \
	if [ -z "$$PKGS" ]; then \
		echo "no changed Go packages"; \
	else \
		echo "changed packages: $$PKGS"; \
		go vet $$PKGS && \
		golangci-lint run $$PKGS && \
		go test -count=1 -cover $$PKGS && \
		echo "=== scan pass ==="; \
	fi
