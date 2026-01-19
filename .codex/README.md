# Codex Sandbox Environment

This directory contains scripts and configuration for the Codex sandbox environment.

## Quick Start

```bash
source .codex/activate.sh
bash .codex/validate.sh
```

## Scripts

### `diagnose.sh` - Environment diagnostic
Check tool availability, versions, and report issues.

**Usage:**
```bash
bash .codex/diagnose.sh
```

**What it checks:**
- Core tools (go, git)
- QA tools (golangci-lint, govulncheck, mage)
- Search tools (rg, snipe)
- Environment variables (GOCACHE, GOMODCACHE, PATH)
- Snipe index health
- Prebuilt Linux binaries

**If issues are found:** Copy the full output and report it so we can fix missing tools or binaries.

### `setup.sh` - Initial environment setup
One-time setup run by Codex on first start.

**What it does:**
- Installs Go toolchain (1.24), golangci-lint, mage
- Installs ripgrep (required for snipe search)
- Links prebuilt Linux binaries from `.codex/bin/linux-amd64/`

**Usage:**
```bash
bash .codex/setup.sh
```

### `activate.sh` - Activate environment
Add repo-local tools to PATH and set cache environment variables.

**Usage:**
```bash
source .codex/activate.sh
```

### `validate.sh` - Full validation (recommended)
Standard QA validation for most Codex tasks.

**What it does:**
1. Auto-activates environment
2. Runs `mage qa` (lint + tests)
3. Optionally runs snipe doctor

**Duration:** ~60s (cached)

**Usage:**
```bash
bash .codex/validate.sh
```

### `smoke-tests.sh` - Ultra-fast validation
Quick pre-commit check (<10s).

**Usage:**
```bash
bash .codex/smoke-tests.sh
```

## Directory Structure

```
.codex/
├── README.md              # This file
├── setup.sh               # One-time setup
├── activate.sh            # Environment activation
├── validate.sh            # Full QA validation
├── smoke-tests.sh         # Fast pre-commit check
├── agent-config.yaml      # Codex workflow config
│
├── bin/                   # Prebuilt Linux binaries
│   └── linux-amd64/
│       ├── golangci-lint
│       ├── govulncheck
│       ├── mage
│       ├── rg
│       └── snipe
│
└── cache/                 # Runtime caches (gitignored)
    ├── go-build/
    ├── mod/
    └── golangci-lint/
```

## Environment Variables

- `GOCACHE` - Go build cache location
- `GOMODCACHE` - Go module cache location
- `GOLANGCI_LINT_CACHE` - Linter cache location
- `GOMAXPROCS` - CPU count (auto-detected)

## Troubleshooting

**Run diagnostics first:**
```bash
bash .codex/diagnose.sh
```

If issues are found, the output will list what's missing and how to fix it.

**Reporting issues:** If you cannot resolve issues yourself, copy the full diagnostic output and report it. Include:
1. Full output from `bash .codex/diagnose.sh`
2. What command failed
3. Any error messages

## See Also

- `README.md` - Project overview
- `AGENTS.md` - Codex rules and test strategy (if present)
