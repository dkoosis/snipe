# snipe impact — Design Doc

## Problem

When modifying a symbol, Claude spends 30-60% of tool calls on defensive exploration — reading callers, grepping references, checking interfaces — to build a mental blast radius before acting. `snipe impact` replaces that with one call.

## Command Interface

```
snipe impact <symbol>        # by name
snipe impact --at file:L:C   # by position
snipe impact --id <hex>      # by ID
```

Hex IDs (16-char) auto-detected from positional args, matching all other commands.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--direct` | false | 1-hop only (skip transitive callers and transitive tests) |

All global flags inherited: `--limit`, `--format`, `--no-body`, `--signature-only`, `--select`

### Cobra Registration

```go
var impactCmd = &cobra.Command{
    Use:     "impact [symbol|id]",
    Short:   "Show blast radius for changing a symbol",
    GroupID: "core",
    Args:    cobra.MaximumNArgs(1),
    RunE:    runImpact,
}
```

## Symbol Resolution

Standard pattern (same as tests, callers, impl):

1. `--id` flag → use directly
2. `--at file:L:C` → `ParsePosition` → `FindSymbolAtPosition`
3. Positional arg → hex auto-detect (16-char) or `LookupByName`
4. Ambiguous (multiple matches) → return disambiguation error with candidates
5. Not found → return structured error

## Query Phases

Three phases, merged into one flat result set with hint-based classification.

### Phase 1: Callers (transitive call graph)

Reuse the tests command CTE pattern but without the `_test.go` filter.

- Direct callers (hop 1) — hint: `direct_caller`
- Transitive callers (hop 2, deduped against hop 1 via `NOT IN`) — hint: `transitive_caller`
- SQL: CTE with `call_graph` self-join, same structure as `FindTests` but matching all symbols
- When `--direct` is set: skip the transitive CTE entirely

### Phase 2: Interface/implementer relationships

- If target is an interface method → `FindImplementers()` → hint: `implementer`
- If target implements an interface → find co-implementers → hint: `co_implementer`

~~If target is a type → find callers of all its methods (aggregate) → hint: `method_caller`~~

**Cut.** "Callers of all methods on a type" fans out explosively for widely-used types (e.g., `*Store` with 30 methods × N callers each). The caller phase already captures direct method callers. Revisit only if real usage shows a gap.

### Phase 3: Test coverage

Call `FindTests()` from #108 directly — no reimplementation.

- Direct tests → hint: `direct_test`
- Transitive tests → hint: `transitive_test`

(Hint names match the established convention in `cmd/tests.go`.)

## Cross-Phase Deduplication

A symbol can appear in multiple phases (e.g., a test helper that is both a `transitive_caller` and a `direct_test`). Strategy:

1. Build results map keyed by symbol ID
2. When a symbol appears in a later phase, **merge hints** — append to existing `[]string`
3. Ordering priority for sort stability: `direct_caller` > `transitive_caller` > `implementer` > `co_implementer` > `direct_test` > `transitive_test`
4. Final result list is deduped by ID with merged hints

## Orchestration Pipeline

Follow the standard command pipeline (same as tests, callers):

1. `GetOutputConfig()` + `GetResponseFormat()` + `ApplyFormatOverrides()`
2. Symbol resolution (see above)
3. Phase 1: callers query
4. Phase 2: interface relationships
5. Phase 3: `FindTests()` call
6. Cross-phase dedup + hint merge
7. `output.ScoreAndSort()` + `ApplySelection()`
8. `output.TruncateToTokenBudget()`
9. `query.CheckFileStaleness()`
10. Build `Response[output.Result]` envelope with suggestions

## Output

Standard `Response[output.Result]` envelope. Each result carries hints classifying the relationship type.

```json
{
  "protocol": 1,
  "ok": true,
  "results": [
    {
      "id": "a1b2c3d4e5f67890",
      "name": "IndexSymbols",
      "kind": "function",
      "package": "github.com/example/snipe/internal/index",
      "file": "internal/index/indexer.go",
      "range": { "start": {"line": 42} },
      "signature": "func IndexSymbols(ctx context.Context, db *sql.DB) error",
      "hints": ["direct_caller"]
    }
  ],
  "suggestions": [
    {
      "command": "snipe callers IndexSymbols",
      "description": "Drill into full caller graph",
      "priority": 2
    }
  ],
  "meta": {
    "command": "impact",
    "query": {"symbol": "IndexSymbols"},
    "total": 16,
    "ms": 12
  }
}
```

### Hint Taxonomy

| Hint | Meaning |
|------|---------|
| `direct_caller` | Directly calls the target (hop 1) |
| `transitive_caller` | Calls something that calls the target (hop 2) |
| `implementer` | Implements the interface the target defines |
| `co_implementer` | Also implements the same interface as target |
| `direct_test` | Test function that directly exercises target |
| `transitive_test` | Test function that exercises target through helpers |

### Suggestions

Use the actual `Suggestion` type (`{command, description, priority, condition}`):

- Summary suggestion (always): `description: "Impact: 4 direct callers, 7 transitive, 2 implementers, 3 tests across 5 packages"`
- If zero tests: `command: "snipe tests <symbol>"`, priority 1, condition: `"no_tests"`
- If many transitive callers: `command: "snipe callers --direct <symbol>"`, priority 2

## New Files

| File | Purpose | Est. LOC |
|------|---------|----------|
| `cmd/impact.go` | Command registration, flags, symbol resolution, orchestration, result conversion, suggestions | ~200 |
| `internal/query/impact.go` | `FindImpactCallers()` — caller CTE (phases 1+2), `ImpactRow` type | ~120 |
| `cmd/impact_test.go` | Table-driven tests: each phase individually + combined | ~150 |

Phase 3 (tests) reuses `FindTests()` directly — no new query code needed.

## Integration

- Add `"impact": true` to `knownSubcommands` in `cmd/root.go`
- Register `impactCmd` with `rootCmd.AddCommand()` in `cmd/impact.go` `init()`
- Add impact hint constants to `internal/output/types.go` alongside existing `HintDeprecated` etc.

## Test Strategy

| Test | Verifies |
|------|----------|
| Direct caller resolution | Phase 1 hop-1 results with `direct_caller` hint |
| Transitive caller dedup | Phase 1 hop-2 excludes hop-1 IDs |
| `--direct` flag | Transitive results suppressed |
| Interface method → implementers | Phase 2 `implementer` hint |
| Cross-phase dedup | Same symbol from phases 1+3 gets merged hints |
| Not-found / ambiguous | Structured error responses |
| Summary suggestion accuracy | Counts match actual result set |
| Blackbox: `mage qa` | End-to-end through CLI |

## Out of Scope (YAGNI)

- `--depth N` flag — hardcode 2-hop like tests command; add if real need emerges
- Risk scoring / severity ratings
- Diff prediction
- File-level impact (symbol-level only)
- Recursive interface chains
- "Callers of all methods on type" aggregation (cut — see Phase 2 notes)
- Suggested refactoring strategies
