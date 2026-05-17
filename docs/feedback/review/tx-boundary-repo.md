# tx-boundary — snipe (repo scope)

run-id: ecebe5258308
date: 2026-05-17
linter: tx-boundary
scope: project
findings: 5

## Pre-work

- `head -1 go.mod` → `module github.com/dkoosis/snipe`
- `BeginTx`/`Begin(` in non-vendor: 10 sites, all under `internal/store/{write,schema,sccs,metrics}.go`
- Single backend: SQLite (modernc), one DB connection, mostly-single-writer
- `defer rollbackOnError(tx, &err)` pattern is uniform across every `Begin()` site
- Schema FKs on refs/call_graph/imports/embeddings/symbol_purposes against `symbols(id)`

Overall: `internal/store/` is well-disciplined — every internal write method opens its own tx, defers rollback, commits, and propagates errors. The bug surface is one layer up, in `cmd/index.go`, where the indexer composes ~8 *separately-committed* store methods into one logical "reindex" operation. That's the dominant pattern flagged below.

## Tier scores

| Tier | Score | Notes |
|------|-------|-------|
| P1 multi-write | YELLOW | F1/F2: indexer fan-out across N independent txs |
| P1 check-then-write | GREEN | no SELECT→branch→UPDATE pattern in app code |
| P1 parent-children | GREEN | symbols+refs+edges co-committed inside `WriteIndex` / `WriteIndexIncremental` |
| P2 invariant | YELLOW | F3: incremental_count read outside the tx that bumps it |
| P3 tx-leak | GREEN | `rollbackOnError` deferred at every BeginTx site |

---

## F1 — Full reindex composes 8 independent transactions

- **Function:** `cmd.runIndex` (full-reindex path)
- **File:** `/Users/vcto/Projects/snipe/cmd/index.go:229-297`
- **Pattern:** `multi-write` (cross-method, app-level)
- **Statements:**
  - `cmd/index.go:229` `s.WriteIndex(symbols, refs, edges)` — tx #1 (symbols, refs, call_graph, plus preserve/restore of embeddings + purposes)
  - `cmd/index.go:234` `s.WritePackageDocs(pkgDocs)` — tx #2
  - `cmd/index.go:239` `s.WriteImports(imports)` — tx #3
  - `cmd/index.go:278` `s.WriteLiterals(literals, absDir)` — tx #4
  - `cmd/index.go:283` `s.WriteFiles(files)` — tx #5
  - `cmd/index.go:288` `s.SetMeta("fingerprint", ...)` — tx #6
  - `cmd/index.go:291` `s.SetMeta("indexed_at", ...)` — tx #7
  - `cmd/index.go:296-297` `s.SetMeta("incremental_count","0")`, `s.SetMeta("orphaned_refs","0")` — txs #8/#9 (errors ignored, see F4)
- **Partial-failure mode:** SIGKILL / panic / disk-full between txs leaves the index in a mixed-generation state: e.g. new `symbols` + new `refs` + new `call_graph` (from tx#1) but **old** `imports`, **old** `package_docs`, **old** `string_refs`, **old** `files` row → next run's change-detection (`fingerprint` + per-file `hash` comparison) misses files because `files` and `fingerprint` disagree with what's actually in `symbols`. Symptom: phantom "no changes" skips, or refs pointing at a symbol whose import row says "package not loaded".
- **Code (abridged):**
  ```go
  if err := s.WriteIndex(symbols, refs, edges); err != nil { ... }      // tx 1
  if err := s.WritePackageDocs(pkgDocs); err != nil { ... }             // tx 2
  if err := s.WriteImports(imports); err != nil { ... }                 // tx 3
  // ... metrics computations (each its own tx via WriteGraphMetrics) ...
  if err := s.WriteLiterals(literals, absDir); err != nil { ... }       // tx 4
  if err := s.WriteFiles(files); err != nil { ... }                     // tx 5
  if err := s.SetMeta("fingerprint", fp.Combined); err != nil { ... }   // tx 6
  if err := s.SetMeta("indexed_at", ...); err != nil { ... }            // tx 7
  ```
- **Fix:** Two viable shapes.
  1. Promote: add `Store.WriteFullIndex(ctx, payload)` that takes the whole payload struct, opens ONE `BeginTx`, calls the existing `writeSymbols/writeRefs/writeCallEdges/writeImports/writePackageDocs/writeLiterals/writeFiles` helpers against that single `*sql.Tx`, then writes `fingerprint`+`indexed_at`+`incremental_count=0`+`orphaned_refs=0` in the same tx. Commit last. (The lowercase helpers already take `*sql.Tx`, so this is mostly an API-shape change.)
  2. Document partial-state acceptability: if the contract is "fingerprint advances last so a partial reindex looks stale on next run and re-runs from scratch", make that explicit at the call site and confirm `runIndex` always re-runs cleanly from a mid-state. Today, `WriteIndex`'s preserve/restore-of-embeddings is itself only safe if `symbols` rewrite and embeddings restore are in the same tx — which they are — but downstream `imports`/`literals` mismatched against `symbols` is not similarly contained.

## F2 — Incremental reindex composes 5 independent transactions

- **Function:** `cmd.runIncrementalIndex`
- **File:** `/Users/vcto/Projects/snipe/cmd/index.go:693-722`
- **Pattern:** `multi-write` (cross-method, app-level)
- **Statements:**
  - `cmd/index.go:693` `s.WriteIndexIncremental(...)` — tx #1 (symbols+refs+edges+imports+orphan sweep+meta(orphaned_refs,incremental_count))
  - `cmd/index.go:699` `s.WritePackageDocs(pkgDocs)` — tx #2
  - `cmd/index.go:706` `s.WriteLiteralsForFiles(...)` — tx #3
  - `cmd/index.go:716` `s.WriteFiles(files)` — tx #4
  - `cmd/index.go:721` `s.SetMeta("indexed_at", ...)` — tx #5
- **Partial-failure mode:** Same shape as F1 but on the hot path (runs every save). A crash after tx #1 commits but before tx #4 leaves `symbols/refs/edges/imports` updated for changed files but `files.hash` still pointing at the previous content. Next invocation's hash diff says "nothing changed" and the user sees stale refs — exactly the symptom that motivated the orphan-sweep work in `WriteIndexIncremental` (`write.go:543-563`). The orphan sweep is correctly internal to that tx; the *outer* composition isn't.
- **Code:** see F1 — same pattern, fewer txs.
- **Fix:** Same as F1. Easier here because `WriteIndexIncremental` already takes most of the payload in one call; folding `WritePackageDocs` / `WriteLiteralsForFiles` / `WriteFiles` / `SetMeta("indexed_at",...)` into the same tx is mostly threading `*sql.Tx` through three existing helper functions.

## F3 — `incremental_count` read-modify-write split across tx boundary

- **Function:** `Store.WriteIndexIncremental`
- **File:** `/Users/vcto/Projects/snipe/internal/store/write.go:451-563`
- **Pattern:** `check-then-write` (counter race)
- **Statements:**
  - `write.go:452` `countStr, _ := s.GetMeta("incremental_count")` — read **outside** the tx
  - `write.go:457` `incCount++`
  - `write.go:473` `tx, err := conn.BeginTx(...)` — tx opens **after** the read
  - `write.go:561` `tx.Exec("INSERT OR REPLACE INTO meta (key,value) VALUES ('incremental_count', ?)", incCount)`
- **Partial-failure mode:** Two concurrent indexers (e.g. editor save races CLI `snipe index`) both read `incremental_count=N`, both write `N+1`. SQLite + WAL serializes the txs so neither write fails, but `incremental_count` advances by 1 instead of 2 → operator-facing counter under-reports drift and the "rebuild when count > threshold" heuristic fires late. Same shape applies if used to gate auto-reindex.
- **Code:**
  ```go
  countStr, _ := s.GetMeta("incremental_count")
  incCount := 0
  if countStr != "" { fmt.Sscanf(countStr, "%d", &incCount) }
  incCount++
  // ... 20 lines later ...
  conn, _ := s.db.Conn(...); tx, _ := conn.BeginTx(...)
  // ... writes ...
  tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('incremental_count', ?)`, ...)
  ```
- **Fix:** Read inside the tx, OR write atomically:
  ```sql
  INSERT INTO meta(key,value) VALUES('incremental_count','1')
    ON CONFLICT(key) DO UPDATE SET value = CAST(meta.value AS INT) + 1
  ```
  The SQL form is preferred — no app-side read at all, race-free regardless of isolation. Note that snipe is single-writer in practice today, so this is P2 (latent), not P1 (live bug).

## F4 — `SetMeta` errors discarded after full reindex

- **Function:** `cmd.runIndex`
- **File:** `/Users/vcto/Projects/snipe/cmd/index.go:296-297`
- **Pattern:** `tx-commit-without-error-check` (variant — error from a tx-using helper dropped)
- **Statements:**
  - `cmd/index.go:296` `_ = s.SetMeta("incremental_count", "0")`
  - `cmd/index.go:297` `_ = s.SetMeta("orphaned_refs", "0")`
- **Partial-failure mode:** `SetMeta` runs a full `INSERT OR REPLACE` via the shared DB handle (no enclosing tx — `schema.go:491`). If it fails (lock contention, disk-full, IO error) the index ships with stale `incremental_count`/`orphaned_refs` meta. Next session's `bd prime`-style readout shows trustworthy-looking counters that don't match reality. Every *other* `SetMeta` call in this function is error-checked; only the last two reset-to-zero calls are discarded.
- **Code:**
  ```go
  if err := s.SetMeta("fingerprint", fp.Combined); err != nil { return ... }   // checked
  if err := s.SetMeta("indexed_at", ...); err != nil { return ... }            // checked
  _ = s.SetMeta("incremental_count", "0")                                       // dropped
  _ = s.SetMeta("orphaned_refs", "0")                                           // dropped
  ```
- **Fix:** Check and return-or-warn like the siblings. If swallowing is intentional ("counters are best-effort"), make that explicit: `// best-effort: counters re-derive next run` plus a `fmt.Fprintf(os.Stderr, "warning: ...")`. The current bare `_ =` is indistinguishable from forgetting to check.

## F5 — Delete-only fast path commits across three txs

- **Function:** `cmd.runDeleteOnlyIndex`
- **File:** `/Users/vcto/Projects/snipe/cmd/index.go:738-756`
- **Pattern:** `multi-write` (smaller-scale repeat of F2)
- **Statements:**
  - `cmd/index.go:744` `s.WriteIndexIncremental(nil, nil, nil, nil, nil, changes.Deleted)` — tx #1
  - `cmd/index.go:750` `s.WriteLiteralsForFiles(nil, nil, changes.Deleted, "")` — tx #2
  - `cmd/index.go:755` `s.SetMeta("indexed_at", ...)` — tx #3
- **Partial-failure mode:** Crash after tx #1 leaves `symbols/refs/imports/files` rows for the deleted files removed, but `string_refs` still references them → next `snipe lits` / `search` query returns hits in files that no longer exist on disk. `indexed_at` separately drifts (informational, lower-impact).
- **Code:**
  ```go
  incResult, err := s.WriteIndexIncremental(nil, nil, nil, nil, nil, changes.Deleted)
  if err != nil { return ... }
  if err := s.WriteLiteralsForFiles(nil, nil, changes.Deleted, ""); err != nil { return ... }
  if err := s.SetMeta("indexed_at", ...); err != nil { return ... }
  ```
- **Fix:** Same shape as F1/F2 fix — fold `string_refs` deletion and `indexed_at` SetMeta into the `WriteIndexIncremental` tx (or into a new `WriteDeleteOnly(deletedFiles)` Store method that scopes one tx around all three operations).

---

## Not flagged (and why)

- **`WriteGraphMetrics` chain in `runIndex` (`cmd/index.go:803-1060`)**: each metric write is independent (PageRank, HITS, abstractness, LCOM4, cyclo, calls-graph) — partial state across these is benign because each `(graph_kind, metric)` row set is self-contained, errors are demoted to warnings, and downstream readers (`snipe metrics --kind=X`) tolerate "metric absent" cleanly. This matches the "don't flag legitimately-independent writes" guidance in the linter rules.
- **`SaveEmbedding` loops** (`cmd/index.go:402-410`, `cmd/embed.go:244-250`): per-symbol embedding saves are documented best-effort streaming writes, errors are logged + skipped, and `WriteIndex`'s preserve/restore step keeps surviving embeddings on next reindex. Wrapping the loop in one tx would multiply memory cost on large repos for no correctness gain.
- **`runMigration` (`internal/store/schema.go:326-383`)**: textbook good — single tx covers DDL + `migrations` row + `meta.schema_version`. Explicitly documented as such in code comments.
- **`store.Open` PRAGMA chain (`internal/store/store.go:50-82`)**: PRAGMAs are session-scoped, not row writes; tx scope doesn't apply.
- **No `check-then-write` race in app code**: searched for `SELECT.*FOR UPDATE` candidate sites; the only read-then-write pattern is the `incremental_count` counter (F3). No money/auth/inventory paths exist.

## Recommendation

Highest leverage: one refactor closes F1, F2, and F5. Introduce `Store.RunIndexWrite(func(tx *sql.Tx) error)` (or equivalent payload-taking variant) and thread the existing lowercase helpers through a single tx. The work is mostly mechanical — `writeSymbols/writeRefs/writeCallEdges/writeImports/writeFiles/insertLiterals` already accept `*sql.Tx`. The only new code is moving `WritePackageDocs`'s body and `SetMeta`'s body to lowercase `tx`-taking variants and exposing the wrapper.
