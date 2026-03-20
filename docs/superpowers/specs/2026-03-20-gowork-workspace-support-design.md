# go.work Workspace Support

**Issue:** #132 — commands only see root module in go.work workspaces
**Date:** 2026-03-20

## Problem

`go list ./...` from a go.work workspace root only enumerates packages in the root module. Packages in sibling workspace modules (e.g., `./graph`, `./store`) are invisible. snipe hardcodes `["./..."]` as the load pattern, so indexing misses all non-root modules.

Verified against trixi (7 modules): `go list ./...` returns 2 packages; explicit per-module patterns return 23.

## Solution

Parse `go.work` at the project root, extract `use` directives, and generate per-module load patterns for `go/packages`.

### New function: `internal/index/workspace.go`

```go
// WorkspacePatterns returns load patterns for go/packages.
// If go.work exists at dir, returns a pattern per workspace module.
// Otherwise returns ["./..."].
func WorkspacePatterns(dir string) ([]string, error)
```

**Logic:**
1. Check if `go.work` exists at `dir`
2. If yes: parse with `golang.org/x/mod/modfile.ParseWork`
3. For each `Use` directive, generate `<use-path>/...` pattern (e.g., `use ./graph` → `./graph/...`)
4. If no `go.work`: return `["./..."]` (current behavior unchanged)

One pattern per `use` directive. No base pattern, no dedup — the `use` directives are the complete list of modules.

### Call sites

**Primary: `internal/index/loader.go`** — the loader's default fallback (lines 59-61) should resolve workspace patterns when `Patterns` is empty. This makes it defensive: any caller that omits patterns gets workspace-aware behavior automatically.

```go
if len(cfg.Patterns) == 0 {
    cfg.Patterns, _ = WorkspacePatterns(cfg.Dir)
    if len(cfg.Patterns) == 0 {
        cfg.Patterns = []string{"./..."}
    }
}
```

**Secondary: `cmd/index.go`** — also passes explicit patterns. Update to use `WorkspacePatterns(absDir)` instead of hardcoded `[]string{"./..."}`.

### Why nothing else changes

- `go/packages` resolves cross-module types when all patterns are loaded in one call
- Symbol extraction, refs, call graph, imports all operate on `[]packages.Package` — module-agnostic
- Store schema already has `pkg_path` per symbol — multi-module data coexists
- Query layer uses `pkg_path` for lookups — works across modules
- Fingerprinting already hashes `go.work` — cache invalidation works
- `FindProjectRoot` finds `.git` at workspace root — correct for go.work repos

### Assumptions

- All `use` directives are children of the git root (standard monorepo layout). `use ../sibling` (directories outside the repo) is out of scope — would require multi-root file walking, which is a different problem.

### Edge cases

| Case | Behavior |
|------|----------|
| No `go.work` | Returns `["./..."]`, zero impact |
| `use .` present | Generates `./...` — one pattern like any other |
| Malformed `go.work` | Return error (don't silently fall back) |
| `use ./sub` with nested pkgs | `./sub/...` picks up all nested packages |
| `use` dir doesn't exist | Let `go/packages` surface the error |
| `GOWORK=off` in env | `go.work` file still parsed; `go/packages` will ignore workspace semantics — acceptable since explicit per-module patterns still load correctly |

## Files

| File | Change |
|------|--------|
| `internal/index/workspace.go` | New — `WorkspacePatterns()` |
| `internal/index/workspace_test.go` | New — unit tests |
| `internal/index/loader.go` | Default pattern fallback uses `WorkspacePatterns` |
| `cmd/index.go` | Call `WorkspacePatterns` instead of hardcoded pattern |

## Testing

- Unit: `WorkspacePatterns` with synthetic `go.work` in temp dir
- Unit: no-go.work returns `["./..."]`
- Unit: malformed go.work returns error
- Unit: `use` dir that doesn't exist (still returns pattern, go/packages errors)
- Manual: run `snipe index` on trixi, verify all 23 packages indexed

## Non-goals

- `FindProjectRoot` changes (already works — `.git` and `go.work` co-located)
- Per-command changes (all query the index DB — fixed by fixing indexing)
- Cross-repo test fixtures (trixi is external)
- `use ../sibling` support (outside git root)
