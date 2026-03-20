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

One pattern per `use` directive. No base pattern, no dedup — the `use` directives are the complete list of modules. `replace` directives in `go.work` are irrelevant (they affect module resolution, not package discovery) and are ignored.

### Call sites

**Primary: `cmd/index.go`** — line 138 hardcodes `Patterns: []string{"./..."}`, which is the direct cause of #132. Replace with `WorkspacePatterns(absDir)`. Error from `WorkspacePatterns` should be surfaced (fail loud — a malformed `go.work` is a real problem the user needs to know about).

```go
patterns, err := WorkspacePatterns(absDir)
if err != nil {
    return fmt.Errorf("resolve workspace patterns: %w", err)
}

result, err := index.Load(index.LoadConfig{
    Context:  GetContext(),
    Dir:      absDir,
    Patterns: patterns,
    Tests:    true,
})
```

**Defensive: `internal/index/loader.go`** — the loader's default fallback (lines 59-61) should resolve workspace patterns when `Patterns` is empty. This ensures any future caller that omits patterns gets workspace-aware behavior automatically. Error is swallowed here (graceful degradation — the fallback to `["./..."]` is better than failing on an optional code path).

```go
if len(cfg.Patterns) == 0 {
    cfg.Patterns, _ = WorkspacePatterns(cfg.Dir)
    if len(cfg.Patterns) == 0 {
        cfg.Patterns = []string{"./..."}
    }
}
```

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
| Malformed `go.work` | `cmd/index.go`: return error (fail loud). `loader.go` fallback: swallow error, degrade to `["./..."]` |
| `use ./sub` with nested pkgs | `./sub/...` picks up all nested packages |
| `use` dir doesn't exist | Let `go/packages` surface the error |
| `GOWORK=off` in env | `go.work` file still parsed, but `go/packages` will not resolve cross-module dependencies. Per-module patterns will fail for packages outside the root module. Acceptable: user explicitly opted out of workspaces. Not our problem to fix. |

## Files

| File | Change |
|------|--------|
| `internal/index/workspace.go` | New — `WorkspacePatterns()` |
| `internal/index/workspace_test.go` | New — unit tests |
| `cmd/index.go` | Call `WorkspacePatterns` instead of hardcoded pattern (primary fix) |
| `internal/index/loader.go` | Default pattern fallback uses `WorkspacePatterns` (defensive) |
| `go.mod` | `golang.org/x/mod` promoted from indirect to direct (already in dependency tree) |

## Testing

- Unit: `WorkspacePatterns` with synthetic `go.work` in temp dir
- Unit: no-go.work returns `["./..."]`
- Unit: malformed go.work returns error
- Unit: `use` dir that doesn't exist (still returns pattern, go/packages errors)
- Blackbox: multi-module fixture with `go.work`, verify `snipe index` + `snipe def` finds symbols from all modules
- Manual: run `snipe index` on trixi, verify all 23 packages indexed

## Non-goals

- `FindProjectRoot` changes (already works — `.git` and `go.work` co-located)
- Per-command changes (all query the index DB — fixed by fixing indexing)
- Cross-repo test fixtures (trixi is external)
- `use ../sibling` support (outside git root)
- Handling `GOWORK=off` gracefully (user's explicit choice)
