# Claude Sandbox Environment

This directory contains configuration and scripts optimized for Claude's cloud sandbox environment.

## Quick Start

Claude Code web sessions automatically activate the environment via the `SessionStart` hook.

Manual activation:
```bash
source .claude/activate.sh
```

## What's Included

### Scripts

#### `SessionStart` - Auto-activation hook
Runs automatically when Claude Code web sessions start. Activates the development environment and displays status.

#### `activate.sh` - Environment activation
Sets up PATH, caches, and performance tuning for the snipe development environment.

**Usage:**
```bash
source .claude/activate.sh
```

#### `setup.sh` - One-time setup
Initial environment setup for Claude sandbox. Installs tools, downloads dependencies, and builds the snipe index.

**What it does:**
- Links prebuilt binaries from `.codex/bin/` (shared resources)
- Installs Go 1.25+, golangci-lint if missing
- Downloads Go modules
- Builds snipe binary if needed
- Verifies/builds snipe index

**Usage:**
```bash
bash .claude/setup.sh
```

#### `quick-check.sh` - Fast validation
Ultra-fast smoke tests (<10s) for rapid feedback.

**What it does:**
- Runs `go vet ./...`
- Runs short tests on key packages
- Verifies build
- Checks snipe index exists

**Usage:**
```bash
bash .claude/quick-check.sh
```

### Configuration

#### `config.yaml` - Sandbox metadata
Structured configuration with:
- Common commands reference
- Tool requirements
- Environment variables
- Validation workflows
- Performance tuning settings

See the file for complete documentation.

### Resource Sharing

The `.claude/` setup **shares binaries** from `.codex/bin/` to avoid duplication:

```
Shared resources:
  .codex/bin/linux-amd64/snipe          → ./bin/snipe
  .codex/bin/linux-amd64/golangci-lint  → ./bin/golangci-lint
  .codex/bin/linux-amd64/govulncheck    → ./bin/govulncheck
  .codex/bin/linux-amd64/rg             → ./bin/rg

Isolated resources:
  .claude/cache/go-build/        (Claude-specific Go build cache)
  .claude/cache/mod/             (Claude-specific Go module cache)
  .claude/cache/golangci-lint/   (Claude-specific linter cache)
```

This design:
- Avoids duplicating large binaries
- Keeps caches isolated between Codex and Claude
- Allows independent cache management

## Snipe Index

The snipe index (`.snipe/index.db`) is **pre-built and committed** to the repository for instant Claude session startup.

**Rebuild index:**
```bash
snipe index
```

**Verify index:**
```bash
snipe doctor
```

## Validation Workflows

### Quick Check (<10s)
Fast pre-commit validation:
```bash
bash .claude/quick-check.sh
```

### Full Validation (~60s)
Complete QA before PR (runs lint + tests + build):
```bash
# Recommended: uses .codex/validate.sh (auto-activates environment)
bash .codex/validate.sh

# Or run directly:
make ci
```

### Custom Validation
```bash
source .claude/activate.sh
go test ./...
golangci-lint run
```

## Environment Variables

### Set automatically by `activate.sh`:

- `GOCACHE` - Go build cache (`.claude/cache/go-build`)
- `GOMODCACHE` - Go module cache (`.claude/cache/mod`)
- `GOLANGCI_LINT_CACHE` - Linter cache (`.claude/cache/golangci-lint`)
- `GOMAXPROCS` - CPU count (auto-detected)
- `GOTOOLCHAIN` - Auto toolchain management
- `PATH` - Includes `./bin` and `.codex/bin/linux-amd64`

### User-configurable:

- `CLAUDE_ACTIVATE_QUIET` - Set to any value to suppress activation output (useful for automation)

## Directory Structure

```
.claude/
├── README.md              # This file
├── config.yaml            # Sandbox configuration metadata
├── SessionStart           # Auto-run hook for web sessions
├── activate.sh            # Environment activation
├── setup.sh               # One-time setup script
├── quick-check.sh         # Fast smoke tests
│
└── cache/                 # Runtime caches (gitignored)
    ├── go-build/          # Go build cache
    ├── mod/               # Go module cache
    └── golangci-lint/     # Linter cache
```

## Comparison: Claude vs Codex

| Feature | Claude | Codex |
|---------|--------|-------|
| **Binaries** | Shared from `.codex/bin` | `.codex/bin/linux-amd64/` |
| **Caches** | Isolated in `.claude/cache/` | Isolated in `.codex/cache/` |
| **Setup** | `.claude/setup.sh` | `.codex/setup.sh` |
| **Activation** | `.claude/activate.sh` | `.codex/activate.sh` |
| **Validation** | `.claude/quick-check.sh` | `.codex/validate.sh`, `.codex/smoke-tests.sh` |
| **Auto-start** | `SessionStart` hook | N/A |
| **Index** | Pre-built, committed | Built on setup |

## Performance Tuning

Optimizations for constrained sandbox environments:

- **Build parallelism**: `-p 1` reduces RAM spikes
- **File descriptors**: Increased to 65536 if possible
- **CPU detection**: Auto-set `GOMAXPROCS`
- **Isolated caches**: Prevents cross-contamination
- **Pre-built index**: Instant startup

## Quick Reference

```bash
# Check environment status
source .claude/activate.sh

# Run tests
go test ./...
go test -short ./...

# Lint code
golangci-lint run

# Build snipe
go build -o bin/snipe .

# Use snipe
snipe search "query"
snipe index
snipe doctor

# Full QA (lint + tests + build)
bash .codex/validate.sh  # or: make ci
```

## Troubleshooting

**Run diagnostics first:**
```bash
bash .claude/diagnose.sh
```

This checks all tools, versions, and environment setup. If issues are found, the output will list what's missing and how to fix it.

**Reporting issues:** If you cannot resolve issues yourself, copy the full diagnostic output and report it. Include:
1. Full output from `bash .claude/diagnose.sh`
2. What command failed
3. Any error messages

**Environment not activated:**
```bash
source .claude/activate.sh
```

**Tools missing:**
```bash
bash .claude/setup.sh
```

**Index missing:**
```bash
snipe index
```

**Tests failing:**
```bash
go clean -testcache
go test ./...
```

## See Also

- `.codex/README.md` - Codex sandbox documentation
- `config.yaml` - Complete command reference
- `CONTRIBUTING.md` - Development guidelines (if present)
