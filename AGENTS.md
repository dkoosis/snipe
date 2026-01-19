# Codex Agent Instructions

## Environment Setup

**IMPORTANT**: Before running any Go tools, mage, or snipe commands, you MUST first activate the environment:

```bash
source .codex/activate.sh
```

This adds the prebuilt binaries to your PATH. Without this, commands like `mage`, `snipe`, `golangci-lint`, and `govulncheck` will not be found.

Alternatively, use full paths:
- `.codex/bin/linux-amd64/mage qa`
- `.codex/bin/linux-amd64/snipe search "query"`
- `.codex/bin/linux-amd64/golangci-lint run`

## Quick Start

```bash
# Activate environment (required once per session)
source .codex/activate.sh

# Run diagnostics to verify setup
bash .codex/diagnose.sh

# Run full QA (lint + tests)
mage qa

# Run tests only
go test ./...

# Run linter only
golangci-lint run
```

## Available Commands

After activating the environment:

| Command | Description |
|---------|-------------|
| `mage qa` | Full QA: build, test, lint, security scan |
| `mage` | Default: build + lint + test |
| `mage test` | Run unit tests |
| `mage lint` | Run golangci-lint |
| `mage build` | Build snipe binary |
| `go test ./...` | Run all tests |
| `go test -race ./...` | Run tests with race detection |
| `snipe search "query"` | Search codebase |
| `snipe doctor` | Check snipe health |

## Vendored Dependencies

All Go dependencies are vendored in `vendor/`. The sandbox can build and test without network access.

## Test Strategy

1. **Unit tests**: `go test ./...`
2. **Race detection**: `go test -race ./...`
3. **Blackbox tests**: `go test -tags=blackbox ./test/blackbox/...`
4. **Full QA**: `mage qa`

## Common Issues

### "command not found: mage"
Run `source .codex/activate.sh` first, or use `.codex/bin/linux-amd64/mage`.

### Network timeouts during go vet
Expected in sandbox. Use `GOTOOLCHAIN=local` or skip network-dependent operations.

### Build fails with module errors
Dependencies are vendored. Use `go build -mod=vendor ./...` if needed.
