# Snipe Makefile
#
# Primary: scan check audit report deploy doctor
#   scan   — changed pkgs only (fast inner loop)
#   check  — full repo: vet + lint + test + build
#   audit  — everything: +race +fuzz +dupe +nilcheck +deadcode +omitempty +blackbox +eval +vuln
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
        vet lint test race fuzz dupe nilcheck deadcode omitempty blackbox vuln build standard \
        install clean \
        baseline bench eval eval-setup

# Tool pins: every gate tool runs as go run <pkg>@$(VER) or npx <pkg>@$(VER).
REPO_STANDARD_VER := v0.4.0
OMITEMPTYSCALAR_VER := v0.4.0
GOVULNCHECK_VER := v1.9.0
NILAWAY_VER := v0.0.0-20260918162853-acb8859b9031
DEADCODE_VER := v0.52.0
JSCPD_VER := 5.1.2

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

check: vet lint test build standard ## Full repo: vet + lint + test + build + repo-standard
	@echo "=== check pass ==="

build: ## Compile everything
	go build ./...

# Fleet gate: repo-standard, pinned (REPO_STANDARD_VER); bumping the pin is a deliberate PR.
standard: ## Run repo-standard (fleet SDLC checker) against this repo
	go run github.com/dkoosis/repo-standard/cmd/repo-standard@$(REPO_STANDARD_VER)

audit: check race fuzz dupe nilcheck deadcode omitempty blackbox eval vuln ## Exhaustive: check + race + fuzz + dupe + nilcheck + deadcode + omitempty + blackbox + eval + vuln
	@echo "=== audit pass ==="

deploy: install ## Build, install, and verify (install reports the path and version)

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

# Runs each Fuzz function it finds, FUZZTIME apiece.
FUZZTIME ?= 10s
fuzz: ## Fuzz every Fuzz test (FUZZTIME=10s each)
	@set -e; \
	for pkg in $$(go list ./...); do \
		list=$$(go test -list '^Fuzz' $$pkg) || exit $$?; \
		for f in $$(printf '%s\n' "$$list" | sed -n '/^Fuzz/p'); do \
			echo "fuzz $$pkg $$f"; \
			go test -run '^$$' -fuzz "^$$f$$" -fuzztime=$(FUZZTIME) $$pkg; \
		done; \
	done

dupe: ## Check for code duplication (jscpd, pinned)
	npx --yes jscpd@$(JSCPD_VER) . --silent --reporters console --ignore '**/.git/**,**/.sandbox/**,**/.beads/**,**/.claude/**,**/vendor/**,**/.snipe/**,**/.eval-repos/**'

nilcheck: ## Run nilaway (pinned)
	go run go.uber.org/nilaway/cmd/nilaway@$(NILAWAY_VER) -include-pkgs=github.com/dkoosis/snipe ./...

# deadcode lists each function no path from main reaches, and exits 0 either way,
# so the target fails on any output.
deadcode: ## Fail on any function unreachable from main (deadcode, pinned)
	@out=$$(go run golang.org/x/tools/cmd/deadcode@$(DEADCODE_VER) .) || { echo "deadcode: could not run"; exit 1; }; \
	if [ -n "$$out" ]; then echo "$$out"; echo "deadcode: the functions above are unreachable; cut them"; exit 1; fi

# omitemptyscalar exits 3 on a ,omitempty tag on a bool (a false lost on write).
omitempty: ## Fail on a bool field tagged omitempty (omitemptyscalar, pinned)
	go run github.com/dkoosis/repo-standard/cmd/omitemptyscalar@$(OMITEMPTYSCALAR_VER) ./...

vuln: ## Scan for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VER) ./...

## ---------------------------------------------------------------------
## Build
## ---------------------------------------------------------------------

install: ## Build and install snipe to $GOBIN (or $GOPATH/bin)
	@go install -ldflags '$(LDFLAGS)' .
	@dest="$$(go env GOBIN)"; dest="$${dest:-$$(go env GOPATH)/bin}"; \
	for dir in /usr/local/bin $$HOME/.local/bin; do \
		[ "$$dir" = "$$dest" ] || rm -f "$$dir/snipe" 2>/dev/null || true; \
	done; \
	echo "installed $$dest/snipe ($$("$$dest/snipe" version 2>&1 | head -1))"

clean: ## Remove build artifacts
	rm -rf .bin bin .snipe .sandbox/bin/linux-amd64 .sandbox/bin/linux-arm64 .sandbox/cache

## ---------------------------------------------------------------------
## Metrics & Eval
## ---------------------------------------------------------------------

baseline: ## Capture performance/quality metrics
	SNIPE_BASELINE=1 go test -v -run TestCaptureBaseline ./test/bench/

bench: ## Run Go benchmarks
	go test -bench=. -benchmem ./test/bench/

# Bump the four SHAs together, then re-run `make eval` (benchmark.yaml and the floors were measured on them).
eval-setup: ## Clone and index benchmark repos, each pinned to a commit
	@go install . && \
	mkdir -p .eval-repos && \
	for repo in "chi:https://github.com/go-chi/chi@9f8adb1aabaa8a91180a821111d3f8e668d1c0bf" "cobra:https://github.com/spf13/cobra@adbc8813901bba65827259daa8e22ff94ec1f30e" "bbolt:https://github.com/etcd-io/bbolt@da9d57a74ae81821605f402975b22d28d95a2b7e" "fzf:https://github.com/junegunn/fzf@96028e2a54023a8ff8240e985ae9c8e4f25c0089"; do \
		name=$${repo%%:*}; spec=$${repo#*:}; url=$${spec%@*}; sha=$${spec#*@}; \
		if [ -d ".eval-repos/$$name" ]; then \
			echo "$$name: already cloned"; \
		else \
			echo "$$name: cloning..."; \
			git init -q ".eval-repos/$$name" && \
			git -C ".eval-repos/$$name" fetch -q --depth=1 "$$url" "$$sha" && \
			git -C ".eval-repos/$$name" checkout -q FETCH_HEAD || exit 1; \
		fi; \
		echo "$$name: indexing..."; \
		snipe index "$$(cd .eval-repos/$$name && pwd)" --enrich=false --embed-mode=off || exit 1; \
	done

eval: ## Run localization benchmark
	# No -run filter: the `eval` build tag already scopes this to the eval
	# package, and a name filter silently excludes its regression tests
	# (sn-9gwk) from the merge gate.
	go test -v -tags=eval -timeout=10m ./test/eval/

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
