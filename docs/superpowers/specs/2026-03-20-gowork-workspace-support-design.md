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
3. For each `Use` directive, generate `<dir>/...` pattern
4. Deduplicate: skip `./...` for `use .` since root pattern already covers it
5. Return combined patterns (e.g., `["./...", "./graph/...", "./store/..."]`)
6. If no `go.work`: return `["./..."]` (current behavior unchanged)

### Call site: `cmd/index.go`

Replace hardcoded `[]string{"./..."}` (line ~138) with `index.WorkspacePatterns(absDir)`.

### Why nothing else changes

- `go/packages` resolves cross-module types when all patterns are loaded in one call
- Symbol extraction, refs, call graph, imports all operate on `[]packages.Package` — module-agnostic
- Store schema already has `pkg_path` per symbol — multi-module data coexists
- Query layer uses `pkg_path` for lookups — works across modules
- Fingerprinting already hashes `go.work` — cache invalidation works
- `FindProjectRoot` finds `.git` at workspace root — correct for go.work repos

### Edge cases

| Case | Behavior |
|------|----------|
| No `go.work` | Returns `["./..."]`, zero impact |
| `use .` present | Deduplicated — root `./...` covers it |
| Malformed `go.work` | Return error (don't silently fall back) |
| `use ./sub` with nested pkgs | `./sub/...` picks up all nested packages |

## Files

| File | Change |
|------|--------|
| `internal/index/workspace.go` | New — `WorkspacePatterns()` |
| `internal/index/workspace_test.go` | New — unit tests |
| `cmd/index.go` | One-line: call `WorkspacePatterns` instead of hardcoded pattern |
| `go.mod` | Promote `golang.org/x/mod` from indirect to direct |

## Testing

- Unit: `WorkspacePatterns` with synthetic `go.work` in temp dir
- Unit: no-go.work returns `["./..."]`
- Unit: malformed go.work returns error
- Manual: run `snipe index` on trixi, verify all 23 packages indexed

## Non-goals

- `FindProjectRoot` changes (already works — `.git` and `go.work` co-located)
- Per-command changes (all query the index DB — fixed by fixing indexing)
- Cross-repo test fixtures (trixi is external)
