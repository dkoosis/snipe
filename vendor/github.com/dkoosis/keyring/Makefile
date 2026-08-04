.PHONY: build test test-race vet lint check vuln audit fuzz

# Pinned, not @latest: reproducible lint across machines and CI. Bump
# deliberately and keep .github/workflows/ci.yml's version in lockstep.
GOLANGCI_LINT_VER := v2.12.2
GOVULNCHECK_VER   := latest

build:
	go build ./...

test:
	go test ./...

# Race detector on: keyring is used from concurrent callers (trixi-bot's
# secrets package memoizes a process-wide Store) — catch data races before
# they ship.
test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VER) run ./...

# Local gate — mirrors CI. Run before pushing.
check: vet lint test-race

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VER) ./...

# FuzzQuoteToken exercises the one hand-rolled parser in this package — the
# `security -i` command-line tokenizer quoteToken feeds. Short burst: enough
# to catch a regression, not a CI-hogging campaign.
fuzz:
	go test -fuzz=FuzzQuoteToken -fuzztime=15s .

# Exhaustive validation — everything in check, plus supply-chain and
# tokenizer-fuzz passes. Peers gate their keyring version bumps on this.
audit: check vuln fuzz
	@echo "=== audit pass ==="
