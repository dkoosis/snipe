# Codex sandbox — environment setup and orientation

*Moved here from the root AGENTS.md on 2026-09-09 (sd-9uw2): AGENTS.md is a stub pointer and never content; these are the facts a Codex container needs that the repo does not state anywhere else.*

Go code navigation CLI for LLMs. Static indexing, <50ms queries, JSON output.

## Environment setup

**First:** verify all tools are available. If anything is missing, the setup script did not run.

Setup is handled by `.codex/setup.sh` (auto-discovered by Codex on container creation).
Fallback: `source .codex/activate.sh` (auto-detects platform, links prebuilt binaries from `.bin/linux-{amd64,arm64}/`).

### Required tools

| Tool | Purpose | Example |
|------|---------|---------|
| `snipe` | Go symbol navigation (self-hosted) | `snipe def Open`, `snipe callers FindPackageDeps` |
| `make` | Build system (build/test/lint) | `make`, `make audit` |
| `golangci-lint` | Go linting | `golangci-lint run ./...` |
| `gofumpt` | Strict Go formatting | `gofumpt -w file.go` |
| `goimports` | Fix imports | `goimports -w file.go` |
| `govulncheck` | Vulnerability scanning | `govulncheck ./...` |
| `jq` | JSON processing | `snipe deps --tree \| jq '.results[0].packages'` |

### Available in codex-universal (no install needed)

`go`, `jq`, `rg` (ripgrep), `python3`, `fdfind` (aliased to `fd` by setup)

### Orientation workflow

```bash
source .codex/activate.sh         # activate environment (if not auto-setup)
snipe doctor                      # verify index is healthy
snipe def <Symbol>                # jump to any definition
snipe callers <Symbol>            # find who calls a function
snipe deps <package>              # package dependency topology
snipe deps --tree                 # full project dependency graph
snipe search "pattern"            # text search (uses rg, no index needed)
```

## Conventions the code does not state

- **Error handling:** wrap with context via `fmt.Errorf("context: %w", err)`.
- **Testing:** table-driven, in-memory SQLite for query tests (see `resolve_test.go`).

*Dropped in the move, as derivable: the project-structure tree (`dtree -d 2`), the QA table (the Makefile is the build doc), and the generic task-execution and output rules (`session-protocol.md`, `guard-rails.md`).*
