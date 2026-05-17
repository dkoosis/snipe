# change-smells — repo scope

Module: `github.com/dkoosis/snipe`
Window: last 6mo (541 commits — well above 30-commit warn threshold)
Signals: git co-change, call_graph (3,428 edges, 109 non-test methods), imports graph (0 cycles).

## Scoring

| Tier | Status | Notes |
|------|--------|-------|
| P1 Change (shotgun + divergent) | 🔴 | 4 cross-pkg co-change pairs ≥5 spanning ≥3 pkgs; 2 divergent files |
| P1 Couplers (envy + intimacy + chains) | 🟢 | 0 feature-envy, 0 mutual imports, 0 message chains ≥3 |
| P2 Data (clumps + primitive) | 🔴 | 5 clumps ≥5×, 1 primitive-obsession pkg (query: 53/61 exported funcs take string) |

**Overall: 🔴** — driven by query-feature shotgun pattern and string-typed query API.

---

## P1 — Change preventers

### 1. [F1] `cmd/refs.go + internal/output/types.go + internal/query/lookup.go` — shotgun-surgery

**Diagnosis.** Adding or modifying a query feature consistently requires editing one `cmd/<verb>.go`, plus `internal/query/lookup.go`, plus `internal/output/{types,json}.go`. The triple repeats across refs/callees/callers/def/search/importers.

**Why it matters.** Every new query verb touches ≥3 packages; the change-cost grows linearly with the number of verbs and is now the dominant friction in `snipe` development.

**Evidence (6mo, files co-changing across ≥3 pkgs in same commit).**

| triple (spanning cmd/output/query) | commits |
|---|---|
| `cmd/refs.go` + `internal/output/types.go` + `internal/query/lookup.go` | 10 |
| `cmd/callees.go` + `internal/output/json.go` + `internal/query/lookup.go` | 9 |
| `cmd/callers.go` + `internal/output/json.go` + `internal/query/lookup.go` | 9 |
| `cmd/refs.go` + `internal/output/json.go` + `internal/query/lookup.go` | 9 |
| `cmd/def.go` + `internal/output/types.go` + `internal/query/lookup.go` | 9 |

Also: 62 commits in 6mo touched ≥3 distinct packages. `internal/output/types.go` appears in 31 of them, `internal/query/lookup.go` in 27, `internal/output/json.go` in 23.

**Fix.** Carve a per-verb seam. Either:
- (a) a `query.Verb` interface (`func Run(ctx, args) (Result, error)`) with verbs owning their lookup SQL, result type, and Claude/JSON writers — collapses the triple into one file per verb; or
- (b) one shared `Result` shape that all verbs return (already 80% true via `output.Result`) and a single `writeClaude`/`writeJSON` table-driven on `kind` — eliminates `output/types.go` and `output/json.go` from the per-feature blast radius.

**Tier: P1.**

---

### 2. [F2] `cmd/index.go:1` — divergent-change

**Diagnosis.** `cmd/index.go` (1,228 LOC, 54 commits in 6mo) hosts the `index` command runner *and* every graph-metrics computer (`computeImportPageRank`, `computeImportCoupling`, `computeAbstractness`, `computeLCOM4`, `computeCycloRollups`, `computeImportSCCs`, `computeImportHITS`, `computeImportDegreeAndEigenvector`, `computeCallsGraphMetrics`). Conventional-commit scopes split: `metrics` (6), `graphmetrics` (5), `embed` (2), `index` (2), plus 4 refactors and 8 fixes — ≥4 unrelated verb-clusters.

**Why it matters.** Metrics work and indexing work block each other. Every metric tweak rewrites a file that also owns `runIndex`, `runIncrementalIndex`, embeddings dispatch, and orphan suggestions.

**Evidence.** `cmd/index.go` symbol inventory (selected):
- L66 `runIndex` (core command)
- L365 `generateEmbeddings`, L456 `startBatchEmbeddings` (embedding lane)
- L796–L1174 nine `compute*` graph-metric functions (≈430 LOC of graph math)

**Fix.** Move the `compute*` family to a new `internal/graphmetrics/post.go` (the package already exists per `metrics-instability.txt`) and have `runIndex` call a single `graphmetrics.RunAll(s)`. `cmd/index.go` shrinks to ≈600 LOC and stops co-changing with metrics work.

**Tier: P1.**

---

### 3. [F3] `internal/output/types.go:1` — divergent-change

**Diagnosis.** 53 commits in 6mo, 866 LOC, 30+ result types (Response, Result, PackResult, PackPackageResult, DepsResult, LifecycleResult, LifecycleGroup, LifecycleFunction, SymResult, MethodSummary, FileHeader, …). Conventional-commit scopes span `output`, `lifecycle`, `pack`, `pkg`, `trace`, `query`, `index` — every new verb adds a type here.

**Why it matters.** The file is a magnet for unrelated changes. Renames or shape changes to one result type ripple through review for unrelated features.

**Evidence.** Top 6mo subjects on this file include "BoundaryResult types for snipe boundary", "feat(lifecycle)…", "feat(pack)…", "feat(trace)…", "feat(pkg)…" — five distinct feature axes hitting one file.

**Fix.** Co-locate result types with the verb that produces them: `internal/output/lifecycle.go`, `internal/output/pack.go`, etc. (`json.go` can still own the writers via a `Renderable` interface). Keep only `Response[T]`, `Meta`, `Error`, `Suggestion`, and the `Result`/`Candidate` core in `types.go`.

**Tier: P1.**

---

### 4. [F4] `cmd/callees.go ↔ cmd/callers.go` — shotgun-surgery (in-pkg twin)

**Diagnosis.** These two files co-changed 32 times in 6mo — the most-coupled pair in the repo. They share command shape (flag set, lookup, writer) but each maintains its own copy.

**Why it matters.** Same-pkg pair, so not "shotgun across pkgs" in the strict tier-🔴 sense, but every callers/callees feature pays a 2× tax and divergence has been observed in this audit's own /sweep history.

**Evidence.** `git log --since=6mo` shows 32 commits that touch both `cmd/callers.go` and `cmd/callees.go` together; the next-densest in-pkg pair (`cmd/importers.go ↔ cmd/imports.go`) is 18.

**Fix.** Extract `cmd/graphwalk.go` with `runGraphWalk(direction string)` that both `callers` and `callees` delegate to. Same pattern works for `importers`/`imports`.

**Tier: P1.**

---

## P1 — Couplers

🟢 No findings. Call graph shows zero methods with feature-envy (no method has ≥3 calls into a foreign pkg outweighing own-pkg calls). Import graph has zero cycles (`metrics-cycles.txt`: 0 nontrivial SCCs). No message chains ≥3 in non-test Go.

---

## P2 — Data smells

### 5. [F5] `internal/query` — primitive-obsession

**Diagnosis.** 53 of 61 exported funcs (87%) accept bare `string` for semantically distinct domain concepts: `symbolID`, `name`, `pkgPath`, `modulePath`, `filePath`, `dirPath`, `typeName`, `value`. Argument-order bugs are silent at compile time.

**Why it matters.** Functions like `FindImpactCallers(db, symbolID, direct, limit, offset)` and `FindSimilarSymbols(db, name, maxDistance, maxResults)` share a `(db, string, int, int)` shape; mixing `symbolID` and `name` at a call site won't error. Snipe's own indexer-vs-query split makes this latent — internal-only callers paper over the lack of types.

**Evidence.** From `.snipe/index.db`:
```
internal/query  exported_funcs=61  exported_funcs_with_string_param=53  ratio=86.9%
internal/context exported_funcs=17 string_funcs=13  ratio=76%
internal/store   exported_funcs=7  string_funcs=7   ratio=100%
```
No domain string-types exist (`grep -E '^type [A-Z]\w+ string' internal/query/*.go` returns ∅).

**Fix.** Introduce in `internal/query/types.go`:
```go
type SymbolID string  // 16-char hex
type PkgPath  string  // import path
type FilePath string  // repo-relative
```
Convert hot signatures first: `Find*(db, SymbolID, ...)`, `Find*ByPackage(db, PkgPath, ...)`. Callers in `cmd/` pass values that already have these meanings; conversion cost is trivial.

**Tier: P2.**

---

### 6. [F6] `internal/query/lookup.go + impact.go + imports.go` — data-clumps

**Diagnosis.** The trio `(db, limit, offset)` appears in **18** function signatures and `(db, limit|offset, symbolID)` family in another **14**. These are paging cursors and result handles that have been propagated by hand through the whole query surface.

**Why it matters.** Adding a new pagination field (e.g. `cursor string`, `total bool`) means editing 18+ signatures plus every cmd caller. Same problem class as F1 but mediated by parameters instead of file structure.

**Evidence.** Clump frequency (from signatures table, non-test):

| param triple | occurrences |
|---|---|
| `(db, limit, offset)` | 18 |
| `(db, limit, symbolID)` | 8 |
| `(filePath, fset, pkg)` | 7 |
| `(db, offset, symbolID)` | 6 |
| `(limit, offset, symbolID)` | 6 |
| `(db, name, pkgPath)` | 6 |
| `(db, pkgPath, symbolID)` | 6 |

`(filePath, fset, pkg)` also recurs 7× in `internal/index/*` — second clump cluster, separate domain (AST parsing).

**Fix.** Two structs:
- `type Page struct { Limit, Offset int }` — replaces 18 of 18 above; pair with `type Cursor = SymbolID` for symbol-anchored paging.
- `type ASTLoc struct { FilePath string; Fset *token.FileSet; Pkg *packages.Package }` — collapse the indexer clump.

Conversion is mechanical; pair with F5 for one rename pass.

**Tier: P2.**

---

## Notes

- No `inappropriate-intimacy` despite hot co-change — import graph is clean (acyclic, 0 SCCs). The shotgun pattern is structural (per-verb fan-out), not cyclic.
- `internal/output` has instability 0.167 (`metrics-instability.txt`) — stable, depended-on. That makes F3 painful: ripple cost is real every time a result type moves.
- Findings F1/F4/F6 are facets of the same root cause: query verbs are not first-class. Fixing F1 (verb seam) likely retires F4 and shrinks F6 organically. Recommend tackling F1 first, then re-running this linter.
