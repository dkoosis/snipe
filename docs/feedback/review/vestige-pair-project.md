# vestige-pair — project scope

Tier: action

Workspace clean. No vestige pairs found in the snipe Go workspace (`/Users/vcto/Projects/snipe`, excluding `vendor/` and `_test.go`). The rule fires only when an empty-or-near-empty struct is paired with a lone constructor and both have zero non-test refs. snipe has neither canonical empty structs in production code nor a constructor-having near-empty type without callers.

## Scorecard

| Metric | Value |
|---|---|
| Candidate types scanned | 6 near-empty (single field, no canonical empty) |
| Constructors scanned | 7 (`New*` / `new*`) |
| Findings emitted | 0 |
| Tier:action | 0 |
| Tier:borderline | 0 |

## Scan summary

Canonical empty structs (`type T struct{}`) in workspace code: **0**. All matches live under `vendor/`.

Near-empty single-field struct candidates and why each is **not** a vestige:

| Type | Decl | Status |
|---|---|---|
| `BatchRequestBody` | `internal/embed/batch.go:60` | No constructor; used as field type and composite literal at `internal/embed/batch.go:55,439` |
| `DepDAG` | `internal/context/types.go:21` | `buildDepDAG` ctor called from `internal/context/generate.go:123` |
| `Files` | `internal/context/types.go:196` | Used as struct field in `BootContext` (`internal/context/types.go:8`) |
| `CompareConfig` | `internal/metrics/compare.go:30` | Passed to `Compare` from `cmd/check.go:83` |
| `Graph` | `internal/graphmetrics/graph.go:16` | Multiple methods + callers (`pagerank.go:10`, `hits.go:18`, `topo.go:6`) |
| `cyclesPayload` | `cmd/metrics_cycles.go:23` | Used in `output.Response` envelope at `cmd/metrics_cycles.go:55,58` |

Constructors (`func New*` / `func new*`) and their non-test caller counts:

| Constructor | Non-test callers |
|---|---|
| `output.NewWriter` | 12+ in `cmd/*` |
| `output.NewNotFoundError` | 5+ in `cmd/*` |
| `output.NewAmbiguousError` | 5+ in `cmd/*` |
| `output.NewMissingIndexError` | 1 (`cmd/root.go:431`) |
| `output.NewIndexInProgressError` | 1 (`cmd/root.go:423`) |
| `analyze.NewAnalyzer` | 1 (`internal/query/explain.go:93`) |
| `util.NewFileCache` | 3 (`internal/output/json.go:17`, `cmd/index.go:196,668`) |

Every constructor has at least one non-test caller; no struct decl is unreferenced.

## Findings

None.

→ no action required; re-run after any large feature removal or design pivot
