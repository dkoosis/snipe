# Snipe — Agent Instructions

Go code navigation CLI for LLMs. Static indexing, <50ms queries, JSON output.

## Environment Setup

**First:** Verify all tools are available. If anything is missing, the setup script didn't run.

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

## Project Structure

```
cmd/           CLI commands (def, refs, callers, callees, deps, search, index)
internal/
  index/       go/packages indexing
  query/       symbol lookup, position resolution, dependency topology
  store/       SQLite persistence
  context/     boot context, roles, flows, enrichment
  output/      JSON envelope formatting
  embed/       Voyage AI embeddings (batch + realtime)
test/blackbox/ integration tests
.snipe/        local index (gitignored)
```

## Code Conventions

- **Error handling:** Wrap with context via `fmt.Errorf("context: %w", err)`.
- **Testing:** Table-driven, in-memory SQLite for query tests (see `resolve_test.go`).
- **Output:** `{protocol, ok, results, meta, error}` JSON envelope — all commands.
- **IDs:** 16-char hex, chainable across commands.

## QA

| Command | What | Duration |
|---------|------|----------|
| `make` | build + lint + test | ~15s |
| `make audit` | race, blackbox, golangci, govulncheck | ~90s |
| `go test ./...` | unit tests only | ~10s |

`make audit` is the merge gate. Must pass before commit.

## Task Execution

- Read `docs/progress.md` for current state
- Pick tasks from `.claude/rules/boot.md` "do next"
- Commit after each logical unit that passes `make`
- Prefer small, surgical changes
- Don't add features, abstractions, or refactors beyond what's requested

## Output Rules

- Final code only — no placeholders, no TODOs
- Must compile: `go build ./...`
- Must validate: `make audit`

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
