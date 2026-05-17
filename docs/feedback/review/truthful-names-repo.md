# truthful-names — snipe repo review

Scope: project · Mode: report · Date: 2026-05-17

Reviewed against `truthful-names.md` (P1 receiver/function/package, P2 file/test). Read-only.

## Verdict

Tier: 🟡 — one #1-PageRank package has a generic basename, two store files dump cross-concern code, and `output/json.go` mostly emits non-JSON. No receiver-mismatch hotspots found in top symbols; package vocabularies otherwise honest.

| Tier | Count |
|------|-------|
| P1 receiver-mismatch | 0 |
| P1 imprecise-function-name | 2 |
| P1 package-generic-basename | 1 (top-PageRank) |
| P2 file-basename-mismatch | 4 |
| P2 test-name-mismatch | 0 sampled |

## Findings

### 1. `internal/util` — package-generic-basename (high-centrality)

- **Symbol:** `internal/util` (pkg)
- **Pattern:** `package-generic-basename`
- **Predicted from name:** assorted helpers, low cohesion expected
- **Actual:** two unrelated concerns — atomic file writes and project-root discovery — under one catch-all name. PageRank #1 (0.172), highest in the repo, so the misleading name is maximally amplified.
- **Evidence:** `internal/util/file.go:13` `WriteFileAtomic` (tempfile + fsync + rename); `internal/util/root.go:11` `FindProjectRoot` (walks up for `.git` / `go.mod`). Also imports `FileCache`, `DefaultMaxCachedFiles` used by `internal/output/json.go:16`.
- **Fix:** split into `internal/atomicfile` (file.go + FileCache) and `internal/projectroot` (root.go). Grep-map:
  - `util.WriteFileAtomic` → `atomicfile.Write`
  - `util.NewFileCache` / `util.DefaultMaxCachedFiles` → `atomicfile.NewCache` / `atomicfile.DefaultMaxCached`
  - `util.FindProjectRoot` → `projectroot.Find`

### 2. `internal/output/json.go` — file-basename-mismatch

- **Symbol:** `internal/output/json.go`
- **Pattern:** `file-basename-mismatch`
- **Predicted from name:** JSON envelope rendering
- **Actual:** 48 funcs; one `writeJSON` (json.go:70) and ~14 `writeClaude*` funcs that render Claude-optimized text — the project's *default* format per D1/D4. Filename hides where Claude rendering lives.
- **Evidence:** `internal/output/json.go:79 writeClaude`, `:116 writeClaudeResults`, `:222 writeClaudeMeta`, `:298 writeClaudeSummary`, `:324 writeClaudePack`, `:422 writeClaudeExplain`, `:456 writeClaudeSym`, `:492 writeClaudeDeps`, `:518 writeClaudeTypes`, `:578 writeClaudeLifecycle`, `:642 writeLifecycleSummary`.
- **Fix:** split. New `internal/output/claude.go` for all `writeClaude*` + helpers; keep `json.go` for `writeJSON` + envelope. Optionally `writer.go` for `NewWriter`/`Writer`/`OutputFormat`. No symbol renames needed.

### 3. `internal/store/embed.go` — file-basename-mismatch

- **Symbol:** `internal/store/embed.go`
- **Pattern:** `file-basename-mismatch`
- **Predicted from name:** embeddings-table CRUD
- **Actual:** mostly embeddings, but `GetCalleesForSymbols` (embed.go:124) reads `call_graph` — unrelated to embeddings. Reader looking for callgraph queries won't grep here.
- **Evidence:** `internal/store/embed.go:124` `func (s *Store) GetCalleesForSymbols(...) ... q := \`SELECT caller_id, callee_id FROM call_graph WHERE caller_id IN (...)\``
- **Fix:** move `GetCalleesForSymbols` to `internal/store/callgraph.go` (new) or fold into a `read.go`. Grep-map: no rename required, move only.

### 4. `internal/store/write.go` — file-basename-mismatch

- **Symbol:** `internal/store/write.go`
- **Pattern:** `file-basename-mismatch`
- **Predicted from name:** write/insert/update paths into the store
- **Actual:** mixes writes with reads — `GetAllFiles` (write.go:410) and `GetStats` (write.go:723) are pure SELECTs. The file is the dumping ground for everything store-shaped.
- **Evidence:** `internal/store/write.go:410 GetAllFiles`, `:723 GetStats`. Both `SELECT … FROM …` only.
- **Fix:** move `GetAllFiles` and `GetStats` into a new `internal/store/read.go` (or `files.go` + `stats.go`). No symbol renames.

### 5. `query.CheckIndexState` — imprecise-function-name

- **Symbol:** `internal/query.CheckIndexState`
- **Pattern:** `imprecise-function-name`
- **Predicted from name:** check (verify/assert) and return ok/error
- **Actual:** computes the current fingerprint, compares with stored, and returns a structured `output.IndexState` describing freshness — closer to "inspect" or "report" than "check".
- **Evidence:** `internal/query/state.go:15-16`
  ```go
  // CheckIndexState computes current fingerprint and compares with stored
  func CheckIndexState(db *sql.DB, repoRoot, version string) output.IndexState {
  ```
- **Fix:** rename → `IndexState(db, repoRoot, version) output.IndexState` (noun-returns-noun). Grep-map: `query.CheckIndexState(` → `query.IndexState(`.

### 6. `cmd.OpenStore` — imprecise-function-name

- **Symbol:** `cmd.OpenStore`
- **Pattern:** `imprecise-function-name`
- **Predicted from name:** open and return the store handle
- **Actual:** does that plus four extra concerns — resolves project root, detects index-mismatch at the wrong location, checks for in-progress indexing, and writes user-facing errors through the `*output.Writer`. The name hides the side-effects and the writer dependency.
- **Evidence:** `cmd/root.go:385`
  ```go
  func OpenStore(w *output.Writer, cmdName string) (*store.Store, string, error) {
      ...
      root := util.FindProjectRoot(cwd) ...
      if !dbExists && root != cwd { ... w.WriteError(...) ... }
      // Check if indexing is in progress
  ```
- **Fix:** rename → `ResolveAndOpenStore` (acknowledges root resolution + open) OR keep `OpenStore` but extract the writer-emitting branches into `reportIndexProblem(w, ...)` so `OpenStore` becomes a thin compose. Grep-map (rename option): `cmd.OpenStore(` → `cmd.ResolveAndOpenStore(`.

## Notes (not findings, observed in passing)

- `cmd/lifecycle.go:324 reattachSignatureRefs` — name honestly advertises mutation of the slice; matches body.
- `internal/output/types.go` `SuggestionsFor{Def,Refs,Search,Callers,Callees,Tests,Pack,Ambiguous,Impact}` — package basename `output` covers them; the suggestion fanout is the type's contract.
- `internal/store/sccs.go` `WriteSCCs` / `ReadSCCs` — coherent, file basename matches both funcs.
- Package vocabularies sampled (`store`, `query`, `index`, `output`) — no terminology drift detected (`SymbolRow`, `RefRow`, `CallEdge`, `CallRow` consistently used).
- `go.mod` module path `github.com/dkoosis/snipe` — basename `snipe` is the product name and matches CLI; no `module-name-uninformative`.

## Skipped

- Subtest naming pass (P2 `subtest-name-lies`) — would require reading ~100 `_test.go` files; bundle didn't include them and a sampled set (`lookup_test.go`, `state_test.go`) looked honest. Defer to a dedicated test-name sweep.
- P3 boolean-trap — defer to `/review domain-vocab`.
