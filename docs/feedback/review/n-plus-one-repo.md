# n-plus-one — repo review

Date: 2026-05-17
Scope: project (non-vendor, non-test Go)
Run: ecebe5258308

Linter: N+1 query/RPC — outer call returns N rows, loop body issues per-row I/O parameterized by row identity.

## Summary

| Tier | Count |
|------|------:|
| 🔴 P1 direct | 1 |
| 🔴 P1 indirect | 2 |
| 🔴 P1 nested (N×M) | 1 |
| 🟡 P1 direct | 2 |
| 🟡 P3 preload candidate | 1 |

Findings: 7. Cap: 10.

---

## 1. 🔴 reattachSignatureRefs — one query per file [P1 direct]

- Function: `cmd.reattachSignatureRefs` · `cmd/lifecycle.go:324`
- Outer collection: `refs []query.RefRow` (orphan refs grouped into `wantFiles` set) — `cmd/lifecycle.go:326-332`
- Inner call: `db.Query("SELECT id,name,kind,signature,line_start,line_end FROM symbols WHERE file_path = ?", file)` · `cmd/lifecycle.go:347-351`
- Estimated N: number of distinct files with orphan refs. On full lifecycle commands this can be every file containing a signature-line ref to the target type — typically 5-50, occasionally hundreds for ubiquitous types (`error`, `string`-typed configs).

```go
for file := range wantFiles {
    rows, err := db.Query(`
        SELECT id, name, kind, signature, line_start, line_end
        FROM symbols
        WHERE file_path = ? AND kind IN ('func', 'method')
    `, file)
    ...
    for rows.Next() { ... byFile[file] = append(byFile[file], fr) }
    rows.Close()
}
```

Fix: batch with `WHERE file_path IN (?,?,...)`, then bucket by `file_path` into `byFile`. Same shape as `internal/context/architecture.go:loadPackageDocs` already uses. One query replaces N. Tradeoff is benign: index on `(file_path, kind)` already exists per `internal/store` schema.

Snippet for the replacement query:

```sql
SELECT id, file_path, name, kind, signature, line_start, line_end
FROM symbols
WHERE kind IN ('func','method') AND file_path IN (?,?,...)
```

---

## 2. 🔴 lifecycle classifications → WalkCallers per row → FindCallers per node [P1 nested]

- Function: `cmd.buildLifecycleResult` (and `buildCallerChain`) · `cmd/lifecycle.go:240-265, 395`
- Outer query: classifications produced earlier in the pipeline (one entry per CRUD-classified ref) — every entry has its own `EnclosingID`.
- Inner call: `buildCallerChain(db, c.EnclosingID, callerDepth)` → `lifecycle.WalkCallers` → BFS that does **one `query.FindCallers` per visited symbol** · `internal/lifecycle/callers.go:40`
- Estimated N: classifications often 20-200 for active types; with `callerDepth=2` and ~5 callers/node, that's 200×~10 = ~2000 `FindCallers` queries on a typical `lifecycle SomeType` invocation.

```go
for _, c := range classifications {
    fn := output.LifecycleFunction{
        ...
        Callers: buildCallerChain(db, c.EnclosingID, callerDepth),  // → WalkCallers → N queries
    }
}
```

Fix: two layers, two batchings.
1. Outer: collect all `c.EnclosingID` into a slice, then a single `FindCallersForMany(ids, depth=1)` returning `map[calleeID][]caller`. Build the chain in-memory.
2. Inner (`WalkCallers`): change `FindCallers` to take `[]string` and the BFS frontier hands all current-depth IDs in one batched query rather than one-per-id.

Tradeoff: the BFS visit-set logic stays; only the I/O fan-in changes. Cost-of-batch vs. cost-of-N is clearly N-dominant here (every node is a separate sqlite round-trip).

---

## 3. 🔴 types command — GetTypeInfo per matched symbol [P1 indirect]

- Function: `cmd.runTypesByPackage` · `cmd/types.go:285-304`
- Outer query: `query.FindPackageSymbols` returns all type-kind symbols in a package (filtered to `typeSymbols`).
- Inner call: `query.GetTypeInfo(s.DB(), sym.ID)` per type · `cmd/types.go:299`
- Hidden cost: `GetTypeInfo` itself issues **4+ queries** (`LookupByID`, `GetMethodsForType`, `getEmbedsForType`, `getFieldsForType`) — see `internal/query/types.go:57-100`.
- Estimated N: types per package — usually 5-30, but `snipe types --pkg internal/query` on this repo would be ~40 → ~160+ queries for one user command.

```go
results := make([]output.TypesResult, 0, len(typeSymbols))
for i := range typeSymbols {
    sym := &typeSymbols[i]
    info, err := query.GetTypeInfo(s.DB(), sym.ID)   // 4+ queries each
    ...
}
```

Fix: add `query.GetTypeInfoBatch(db *sql.DB, ids []string) (map[string]*TypeInfo, error)` that runs the four child queries with `WHERE symbol_id IN (...)` / `WHERE receiver IN (...)` and joins in Go. Cuts ~160 queries to ~5 for the package case. The same helper benefits any future "show type details for matches" surface.

---

## 4. 🔴 search enrichment — FindEnclosingSymbol per ripgrep hit [P1 indirect]

- Function: `cmd.runSearch` enrichment block · `cmd/search.go:154-172`
- Outer query: `results` from ripgrep / index lookup.
- Inner call: `query.FindEnclosingSymbol(db, results[i].File, line)` per hit, with `FindSymbolAtPosition` fallback · `cmd/search.go:157, 164`
- Estimated N: typical search caps at 50 hits; semantic search returns up to ~30. Sub-second perceived, but still 50-100 sequential sqlite round-trips on every `snipe search`, on the critical user path.

```go
for i := range results {
    line := results[i].Range.Start.Line
    if enc := query.FindEnclosingSymbol(s.DB(), results[i].File, line); enc != nil { ... }
    else if sym := query.FindSymbolAtPosition(s.DB(), results[i].File, line); sym != nil { ... }
}
```

Fix: batch by `(file_path, line)` tuples. Add `query.FindEnclosingSymbolsBatch(db, []FileLine) map[FileLine]*SymbolRow` — one query with `file_path IN (...)`, group symbols by file in Go, do the line-range pick in-memory. Same trick collapses both `Enclosing` and `AtPosition` branches into one fetch since both consult `symbols.line_start/line_end`.

---

## 5. 🔴 edit batch — LookupByName per request [P1 direct]

- Function: `cmd.runEdit` · `cmd/edit.go:322-337`
- Outer collection: `requests []EditRequest` from CLI JSON input.
- Inner call: `query.LookupByName(db, req.Symbol)` per request · `cmd/edit.go:327`
- Estimated N: edit is explicitly the batched-edits command; typical N = number of edits in the JSON payload, often 5-20. Each call hits `symbols` table with `WHERE name = ?` plus ranking SQL.

```go
for _, req := range requests {
    symbols, err := query.LookupByName(s.DB(), req.Symbol)
    ...
}
```

Fix: gather `req.Symbol` names up front, call new `query.LookupByNames(db, []string) map[string][]SymbolRow`. Inside the loop, just `symbols := byName[req.Symbol]`. This same helper feeds finding #7 below.

---

## 6. 🟡 SaveEmbedding — one INSERT per embedding in a batch [P1 direct]

- Function: `cmd.runIndexEmbedSync` / embed sync · `cmd/index.go:382-413`
- Outer query: `client.Embed(ctx, texts, "document")` returns `embeddings` for a batch of up to `batchSize` symbols (typically 32-128).
- Inner call: `s.SaveEmbedding(batch[j].ID, emb, model)` → one `INSERT OR REPLACE` per embedding · `internal/store/embed.go:11-21`
- Estimated N: total symbols indexed (often 5,000-50,000) → that many individual INSERTs, no surrounding transaction.

```go
for j, emb := range embeddings {
    if emb == nil { continue }
    if err := s.SaveEmbedding(batch[j].ID, emb, client.Model()); err != nil { ... }
}
```

Fix: add `store.SaveEmbeddingsBatch(rows []EmbeddingInsert) error` that wraps a single transaction around the batch (or builds a multi-row `INSERT … VALUES (?,?,?,?),(?,?,?,?)…`). The batch is already bounded by `batchSize` upstream; transaction-per-batch keeps memory steady and avoids per-row fsync overhead. Hot path on every `snipe index`. Tradeoff: blob sizes vary; if a row fails the whole batch rolls back — acceptable since the upstream code already returns on first error.

---

## 7. 🟡 LookupByName — preload candidate across cmd surface [P3 preload]

- Pattern: `query.LookupByName(db, name)` is called from 8+ call sites (`cmd/edit.go`, `cmd/impl.go`, `cmd/tests.go`, `cmd/explain.go`, `cmd/impact.go`, `cmd/pack.go`, `cmd/diagram.go`, `cmd/sym.go`).
- Today only `cmd/edit.go` invokes it from inside a loop (finding #5). The others are single-name CLI commands.
- However: any future "show me info for these N symbols by name" surface (Claude already chains queries; orca's `go_symbol` issues sibling calls) will repeat the N+1 shape.

Fix: add a `LookupByNames(db, []string) (map[string][]SymbolRow, error)` to `internal/query/lookup.go` next to the existing `BatchLookupByID`. Migrate `cmd/edit.go` immediately (closes finding #5). Other callers stay as-is; the helper just exists for future batched flows.

Don't flag: cases like `cmd/diagram.go:331, 622` where the user passed exactly one name — single LookupByName is correct there.

---

## Skipped / not flagged

- `internal/context/flows.go:406` — already batched (`WHERE caller_id IN (...)`), correct.
- `internal/context/architecture.go:loadPackageDocs` — already batched.
- `internal/context/ranking.go:71` — already batched (comment "avoids N+1 query problem by using a LEFT JOIN with COUNT" — accurate).
- `internal/context/conventions.go:178-211` — bounded scans, no per-row I/O.
- `internal/index/literals.go:36, 65` — filesystem read-per-file, not DB; bounded by package size.
- `cmd/pack_package.go:208`, `cmd/orient.go:191` — bounded directory walks (manifest/package files), N ≤ ~20.
- `internal/context/generate.go:1022-1034` — two-pattern probe (N=2), bounded.
- `cmd/tests.go`, `cmd/callers.go`, `cmd/callees.go`, `cmd/impact.go`, `cmd/diagram.go`, `internal/embed/search.go` — explicit `BatchLookupByID` use (✓ correct shape).
