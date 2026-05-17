# SOLID review — repo

Run: 2026-05-17 · scope: project · cap: 10 · linter: solid

## Scorecard

| Tier | LSP | SRP | ISP |
|------|-----|-----|-----|
| Result | 🟢 0 | 🔴 persistence+API+formatting fused on multiple types | 🟡 1 one-impl iface, 1 case-dispatch fat type |

Overall: 🔴 — `*store.Store` mixes ≥5 concerns; `*embed.BatchClient` fuses HTTP + persistence; `*output.Writer` is a 21-method type-switch renderer.

Interface surface is intentionally thin (one production interface, `Embedder`), so LSP is clean by construction. SRP findings dominate.

---

## F1 — `store.Store` god type (SRP)

**Symbol:** `internal/store.Store` (`internal/store/store.go:18`)
**Principle:** SRP — `srp-mixed-concerns`
**Severity:** 🔴

**Evidence:** 31 methods spanning ≥5 distinct concerns:

| Concern | Methods |
|---|---|
| lifecycle / connection | `Open`, `Close`, `DB`, `Path` |
| schema / migration | `initSchema`, `runMigration`, `getCurrentMigrationVersion`, `addPkgPathColumn`, `addFilePathRelColumns`, `addNamePositionColumns` |
| metadata k/v | `GetMeta`, `SetMeta`, `GetPurpose` |
| symbol/ref/edge write | `WriteIndex`, `WriteIndexIncremental`, `WriteImports`, `WriteFiles`, `WritePackageDocs`, `WriteLiterals`, `WriteLiteralsForFiles`, `GetAllFiles`, `GetStats` |
| graph metrics | `WriteGraphMetrics`, `ReadTopN`, `WriteSCCs`, `ReadSCCs` |
| embeddings | `SaveEmbedding`, `GetEmbedding`, `GetAllEmbeddings`, `GetEmbeddingsByPackage`, `GetCalleesForSymbols`, `CountEmbeddings` |

Files: `store.go` (connection), `schema.go` (DDL), `write.go` (815 LOC of bulk writes), `metrics.go`, `sccs.go`, `embed.go`. The split is already half-done at the file level — only the receiver is shared.

```go
// internal/store/store.go:18
type Store struct {
    db   *sql.DB
    path string
}
// 31 methods across 6 files all hang off this one struct
```

**Fix:** keep `*Store` as the connection holder + schema migrator. Promote the file-level groupings to typed sub-stores that wrap the same `*sql.DB`:

- `*Store` keeps: `Open`, `Close`, `DB`, `Path`, schema/migration (kept private), `GetMeta`/`SetMeta`.
- `SymbolStore` (or `IndexWriter`): `WriteIndex*`, `WriteImports`, `WriteFiles`, `WritePackageDocs`, `WriteLiterals*`, `GetAllFiles`, `GetStats`.
- `MetricsStore`: `WriteGraphMetrics`, `ReadTopN`, `WriteSCCs`, `ReadSCCs`.
- `EmbeddingStore`: `SaveEmbedding`, `GetEmbedding`, `GetAllEmbeddings`, `GetEmbeddingsByPackage`, `CountEmbeddings`. (`GetCalleesForSymbols` belongs on `SymbolStore`.)

Construct them with `NewEmbeddingStore(s *Store)`; callers depend on the narrow type. Reduces fan-in for changes — an embedding schema tweak no longer parses 815 LOC of `write.go`.

---

## F2 — `embed.BatchClient` fuses HTTP and on-disk state (SRP)

**Symbol:** `internal/embed.BatchClient` (`internal/embed/batch.go:21`)
**Principle:** SRP — `srp-persistence-on-domain` (analogous: persistence on an API client)
**Severity:** 🟡

**Evidence:** 10 methods, two unmistakable concerns:

| Concern | Methods |
|---|---|
| Voyage AI HTTP client | `UploadFile`, `CreateBatch`, `GetBatchStatus`, `DownloadFile`, `ParseBatchResults`, `Model`, `WriteJSONL` |
| Filesystem state ($stateDir/batch_state.json) | `SaveState`, `LoadState`, `ClearState` |

```go
// internal/embed/batch.go:21
type BatchClient struct {
    apiKey   string
    model    string
    baseURL  string
    client   *http.Client
    stateDir string // ← unrelated to the other four fields
}
// Save/Load/ClearState never touch apiKey/client; UploadFile/CreateBatch never touch stateDir
```

`stateDir` lives on `BatchClient` for convenience, but the state methods don't touch any other field. Changing the on-disk format (e.g. atomic-write tweak in `SaveState` already noted in comments) forces a rebuild of the HTTP client type.

**Fix:** split a `BatchStateStore` with `{SaveState, LoadState, ClearState}` over `stateDir`. `BatchClient` shrinks to HTTP-only (`apiKey`, `model`, `baseURL`, `client`). Caller wires both. `WriteJSONL` could move to a free function or onto `BatchStateStore` since it's also filesystem-only.

---

## F3 — `output.Writer` is a render dispatch table, not a writer (SRP / "fat type")

**Symbol:** `internal/output.Writer` (`internal/output/json.go:38`)
**Principle:** SRP — `srp-mixed-concerns` (rendering concern split across 14 result shapes via type switch)
**Severity:** 🟡

**Evidence:** 21 methods on `*Writer`. The public surface is tiny — `WriteResponse`, `WriteError`, `Elapsed`, `WriteClaudePkgGrouped` — but `writeClaude` is a 14-case type switch dispatching to 14 unexported renderers, and `writeJSON`/`writeHuman` add two more format paths. Every new result type touches `Writer`.

```go
// internal/output/json.go:79
func (w *Writer) writeClaude(resp any) error {
    var b strings.Builder
    switch r := resp.(type) {
    case Response[Result]:        w.writeClaudeResults(&b, ...)
    case Response[Summary]:       w.writeClaudeSummary(&b, ...)
    case Response[PackResult]:    w.writeClaudePack(&b, ...)
    case Response[ExplainResult]: w.writeClaudeExplain(&b, ...)
    case Response[SymResult]:     w.writeClaudeSym(&b, ...)
    // ...11 more cases...
    }
}
```

This is open-closed-violation shaped (every new `Response[T]` edits this switch), but the SRP angle is sharper: `Writer` owns the format dispatch *and* the rendering of every shape. The methods are all "rendering," but the rendering of `LifecycleResult` has nothing to do with the rendering of `BoundaryResult` — they share only the `*strings.Builder` plumbing.

**Fix:** keep `Writer` as a thin dispatcher holding `out/format/start`. Make each per-shape renderer a free function (`renderClaudePack(b, results, meta)`) — they already take all state as parameters. The type-switch stays in one place; tests of pack/sym/lifecycle rendering stop depending on a 1473-LOC file.

If the open-closed shape itself bothers you: register renderers in a `map[reflect.Type]renderFn` at init. Probably overkill for this codebase — the file-level split is the win.

---

## F4 — `embed.Embedder` is single-impl-with-test-mock (ISP)

**Symbol:** `internal/embed.Embedder` (`internal/embed/search.go:22`)
**Principle:** ISP — `interface-with-one-impl`
**Severity:** 🟢 (informational)

**Evidence:** one production impl (`*Client` in `client.go:215`), one test mock (`mockEmbedder` in `search_test.go:17`). No callers outside `Search()`. Interface is one method, so cost is near-zero, but the rule-of-thumb says: introduce the abstraction with the second concrete.

```go
// internal/embed/search.go:22
type Embedder interface {
    EmbedOne(ctx context.Context, text, inputType string) ([]float32, error)
}
```

**Fix:** none required. The interface enables the test mock with no struct-with-funcs gymnastics, and the surface is one method — well below the `isp-fat-interface` threshold. Listed only so reviewers don't independently flag it; the test-mock justification is sufficient.

---

## Notes / not flagged

- **LSP:** no production stub-impls. The three `"not supported"` strings in the repo are user-facing error messages (`cmd/metrics.go`) or API doc comments (`batch.go:59`), not interface-impl evasions.
- **No interface-with-one-impl beyond F4.** The codebase deliberately avoids abstraction towers — there are only ~2 production interfaces total and one test-only interface (`Saver` in `callgraph_test.go`). This matches the project's D9 "no new abstraction towers" stance.
- **`analyze.Analyzer` (6 methods)** and **`diagram.Builder` (6 methods)** were reviewed — both stay within one concern (defect detection / D2 diagram assembly respectively). No SRP finding.
- **`cmd/` package LCOM4 = 43** (highest in repo, per bundle metrics) suggests architectural splitting at the package level, but that is `/review arch` territory, not SOLID.

---

## Summary

3 actionable findings, 1 informational. Pattern: the codebase keeps interface surface deliberately thin (good — no LSP/ISP rot), but several long-lived concrete types accumulated multi-concern method sets organically. The wins are all SRP splits where the file-level structure has already done the analysis for you — `Store` lives in 6 files, `BatchClient` lives in one struct with two unrelated field clusters, `Writer` lives in a 14-case type switch.

Highest leverage: F1 (`Store`). Decomposing it into typed sub-stores would shrink the blast-radius of every future schema or write-path change and let `internal/store/write.go` (currently 815 LOC, the largest non-output file in the repo) split along the same seams.
