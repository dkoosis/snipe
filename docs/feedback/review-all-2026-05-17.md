# /review all — snipe — 2026-05-17

**run_id**: `ecebe5258308`
**linters**: 23 (project scope)
**reports**: `docs/feedback/review/<linter>-repo.md`
**total findings**: 112

## Scorecard

| Linter | Findings | Tier | Top items | Report |
|---|---:|---|---|---|
| alloc-bounds | 0 | 🟢 | no public network surface — threat model doesn't bind | [alloc-bounds](review/alloc-bounds-repo.md) |
| api-surface | 4 | 🟡 | `Embedder` single-impl iface; `ErrStaleIndex`/`(*Store).Path()`/`FileCache.Clear()` exported but unreferenced | [api-surface](review/api-surface-repo.md) |
| arch | 10 | 🔴 | pkg-surface bloat: `output` (175), `query` (155), `context` (128); no `.go-arch-lint.yml` | [arch](review/arch-repo.md) |
| change-smells | 6 | 🔴 | query-verb shotgun (4-file co-change), primitive-obsession in `internal/query` (53/61 funcs) | [change-smells](review/change-smells-repo.md) |
| concurrency-safety | 3 | 🟢 | race-clean; `WriteIndex*`/loader missing `context.Context` propagation | [concurrency-safety](review/concurrency-safety-repo.md) |
| conversion-drift | 0 | 🟢 | category mismatch — diff-scoped linter; project-wide pass found nothing | [conversion-drift](review/conversion-drift-repo.md) |
| ctx-value | 0 | 🟢 | zero first-party `context.WithValue` / `ctx.Value` | [ctx-value](review/ctx-value-repo.md) |
| domain-vocab | 6 | 🟡 | `ApplyFormatOverrides` 10/11 sites pass bare `false`; raw SQL fragments as params | [domain-vocab](review/domain-vocab-repo.md) |
| errors-design | 6 | 🟡 | string-match `"not found"` for `rg` missing; `"no such table"` substring after `errors.As` | [errors-design](review/errors-design-repo.md) |
| goroutine-lifecycle | 8 | 🟡 | `cmd/watch.go:105` reindex goroutine — no ctx-check on `reindexDone` send | [goroutine-lifecycle](review/goroutine-lifecycle-repo.md) |
| io-parallel | 1 | 🟡 | `cmd/index.go:365` sequential Voyage embeddings — ~8× wall-clock win available | [io-parallel](review/io-parallel-repo.md) |
| json-shape | 9 | 🟡 | omitempty on load-bearing zeros (Score, TokenEstimate); 3 decoders allow unknown fields | [json-shape](review/json-shape-repo.md) |
| n-plus-one | 7 | 🔴 | `reattachSignatureRefs` per-orphan query; `WalkCallers` ~2000 queries; `GetTypeInfo` 4+ fanout | [n-plus-one](review/n-plus-one-repo.md) |
| pointer-value | 2 | 🟡 | `PositionQuery` 32-byte struct passed by pointer with no mutation; `Fingerprint` similar | [pointer-value](review/pointer-value-repo.md) |
| slice-map | 7 | 🟡 | `Graph.InEdges` recomputed inside PageRank k-loop; doc-fixes on shared-backing returns | [slice-map](review/slice-map-repo.md) |
| solid | 4 | 🔴 | `store.Store` SRP — 31 methods × 5+ concerns; `BatchClient` + `output.Writer` fused | [solid](review/solid-repo.md) |
| sqlite | 10 | 🟡 | deferred `db.Begin()` everywhere; no ctx propagation; FK-OFF window on incremental | [sqlite](review/sqlite-repo.md) |
| test-effectiveness | 3 | 🟢 | minor evergreen asserts; no mock misuse; no test-without-assertion | [test-effectiveness](review/test-effectiveness-repo.md) |
| test-tables | 10 | 🔴 | 8× legacy `tc := tc` rescopes (dead since go 1.22); 2 one-row tables | [test-tables](review/test-tables-repo.md) |
| truthful-names | 6 | 🟡 | `internal/util` generic-basename (#1 PageRank pkg); `json.go` hides Claude renderers | [truthful-names](review/truthful-names-repo.md) |
| tx-boundary | 5 | 🟡 | full reindex composes 8 independent txs; crash → mixed-generation index | [tx-boundary](review/tx-boundary-repo.md) |
| vestige-pair | 0 | 🟢 | workspace clean — no orphan empty-struct/constructor pairs | [vestige-pair](review/vestige-pair-project.md) |
| zero-sentinel | 5 | 🟡 | `BatchState.UpdatedAt` zero → `time.Since` returns ~17.5M hours → spurious staleness | [zero-sentinel](review/zero-sentinel-repo.md) |

**Tier roll-up**: 🔴 5 · 🟡 14 · 🟢 4

## Top headline issues (cross-linter convergence)

### 1. `cmd/index.go` — composite-tx + n+1 + sequential I/O cluster
Eight linters fire here. Single root: `cmd/index.go` orchestrates the indexing pipeline by calling many `store.*` methods sequentially, each in its own tx, with embed work sequential. **One refactor** (`Store.WriteFullIndex` / `Store.WriteIncremental` wrapping the existing `*sql.Tx` helpers, plus `errgroup` around Voyage batches) collapses findings from tx-boundary (F1/F2/F5), n-plus-one (F6), io-parallel (F1), and zero-sentinel (F1).

### 2. `internal/embed/batch.go` — surface fused with state
Eight linters cite it. `BatchClient` mixes HTTP client + on-disk batch state + zero-sentinel timestamps. solid (F2) and zero-sentinel (F3/F4) want the split; alloc-bounds documents bounded `bufio.Scanner` as borderline; goroutine-lifecycle (F4) confirms the pipe-writer pattern is clean.

### 3. `internal/store/write.go` — boundary file under stress
Seven linters. tx-boundary (F3) flags the `incremental_count` read-modify-write race; errors-design (F2) flags substring error classification; truthful-names flags `GetAllFiles`/`GetStats` parked in a write file; sqlite flags ctx-propagation gaps.

### 4. Cmd-layer architecture seam
arch (10 findings) + solid (F1 store SRP) + change-smells (F1 query-verb shotgun) + truthful-names (F1 `internal/util`) all triangulate the same issue: the cmd → query → store seams are thin abstractions over a fat `Store` with no first-class query verbs. Adding a `query.Verb` seam (change-smells F1) likely retires multiple findings across linters.

## Cross-linter hotspots

_Files cited by 3+ distinct linters. One fix may close multiple findings._

| # linters | file | linters |
|---:|---|---|
| 8 | `cmd/index.go` | change-smells, concurrency-safety, io-parallel, json-shape, n-plus-one, tx-boundary, zero-sentinel, vestige-pair |
| 8 | `internal/embed/batch.go` | alloc-bounds, concurrency-safety, errors-design, goroutine-lifecycle, json-shape, solid, zero-sentinel, vestige-pair |
| 7 | `cmd/root.go` | api-surface, concurrency-safety, domain-vocab, errors-design, goroutine-lifecycle, truthful-names, vestige-pair |
| 7 | `internal/store/write.go` | concurrency-safety, domain-vocab, errors-design, json-shape, sqlite, truthful-names, tx-boundary |
| 6 | `internal/output/json.go` | concurrency-safety, domain-vocab, json-shape, solid, truthful-names, vestige-pair |
| 4 | `cmd/embed.go` | concurrency-safety, io-parallel, tx-boundary, zero-sentinel |
| 4 | `cmd/search.go` | api-surface, domain-vocab, errors-design, n-plus-one |
| 4 | `internal/context/generate.go` | io-parallel, n-plus-one, slice-map, vestige-pair |
| 4 | `internal/store/embed.go` | json-shape, n-plus-one, sqlite, truthful-names |
| 4 | `internal/store/store.go` | api-surface, solid, sqlite, tx-boundary |
| 4 | `internal/util/file.go` | api-surface, concurrency-safety, truthful-names, zero-sentinel |
| 3 | `cmd/lifecycle.go` | n-plus-one, sqlite, truthful-names |
| 3 | `cmd/orient.go` | domain-vocab, json-shape, n-plus-one |
| 3 | `cmd/watch.go` | concurrency-safety, goroutine-lifecycle, json-shape |
| 3 | `internal/output/types.go` | api-surface, change-smells, json-shape |
| 3 | `internal/store/schema.go` | json-shape, sqlite, tx-boundary |

## Line-level hotspots (2+ linters)

| # | location | linter:rule |
|---:|---|---|
| 2 | `cmd/index.go:291` | json-shape, tx-boundary |
| 2 | `cmd/index.go:382` | io-parallel, n-plus-one |
| 2 | `cmd/index.go:693` | concurrency-safety, tx-boundary |
| 2 | `cmd/lifecycle.go:324` | n-plus-one, truthful-names |
| 2 | `cmd/lifecycle.go:347` | n-plus-one, sqlite |
| 2 | `cmd/root.go:94` | concurrency-safety, goroutine-lifecycle |
| 2 | `cmd/root.go:96` | concurrency-safety, goroutine-lifecycle |
| 2 | `cmd/watch.go:95` | concurrency-safety, goroutine-lifecycle |
| 2 | `cmd/watch.go:105` | concurrency-safety, goroutine-lifecycle |
| 2 | `internal/embed/batch.go:172` | concurrency-safety, goroutine-lifecycle |
| 2 | `internal/embed/search.go:22` | api-surface, solid |
| 2 | `internal/output/json.go:17` | concurrency-safety, vestige-pair |
| 2 | `internal/output/json.go:79` | solid, truthful-names |
| 2 | `internal/query/explain.go:93` | slice-map, vestige-pair |
| 2 | `internal/store/write.go:658` | errors-design, sqlite |

## Next steps

- `/assess-feedback <linter>` per row to rate findings & log accept-ratios.
- Note: `trixi log-skill` was reported by every reviewer as `unknown command` — the closing-hook telemetry call is broken on this trixi build. Either add the subcommand or update the harness to use the right invocation.
