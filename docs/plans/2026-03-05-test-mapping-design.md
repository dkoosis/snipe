# Test Mapping: `snipe tests <symbol>` — Design

## Goal

Given a symbol, find the test functions that exercise it. Primary consumer is Claude — when modifying a function, Claude needs to know which tests to run. Completeness matters more than precision (missing a test is worse than running an extra one).

## Command Interface

```
snipe tests <symbol>           # 2-hop transitive (default)
snipe tests --direct <symbol>  # Direct callers only (1-hop)
snipe tests --at file:L:C      # Position-based lookup (uses ResolveSymbolAtPosition)
```

Standard snipe flags: `--format` (including `summary`), `--limit`, `--select`, `--signature-only`, `--no-body`, `--with-body`.

## Approach: Call-graph based

Uses `call_graph` table, not `refs`. Finds tests that actually *call* the symbol, not just mention it. Two modes:

### Test function detection

A symbol is a test function if it lives in a `_test.go` file and matches one of Go's test function prefixes:

```
Test*        — unit/integration tests
Benchmark*   — performance tests
Fuzz*        — fuzz tests
Example*     — example tests (also serve as documentation)
```

All four exercise the code under test. Since our goal is completeness > precision, include all of them.

### Direct (1-hop)

Test functions that directly call the symbol:

```sql
SELECT DISTINCT s.id, s.name, s.kind, s.file_path, s.file_path_rel,
       s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
       s.signature, s.doc, s.receiver, f.hash,
       1 AS hop
FROM call_graph cg
JOIN symbols s ON s.id = cg.caller_id
LEFT JOIN files f ON s.file_path = f.path
WHERE cg.callee_id = :symbol_id
  AND s.file_path GLOB '*_test.go'
  AND (s.name GLOB 'Test*' OR s.name GLOB 'Benchmark*'
       OR s.name GLOB 'Fuzz*' OR s.name GLOB 'Example*')
```

### Transitive (2-hop, default)

Also finds `Test* → helper → Symbol` patterns. Common in Go where test helpers set up fixtures and call the code under test.

```sql
WITH direct_tests AS (
  SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel,
         s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
         s.signature, s.doc, s.receiver, f.hash,
         1 AS hop
  FROM call_graph cg
  JOIN symbols s ON s.id = cg.caller_id
  LEFT JOIN files f ON s.file_path = f.path
  WHERE cg.callee_id = :symbol_id
    AND s.file_path GLOB '*_test.go'
    AND (s.name GLOB 'Test*' OR s.name GLOB 'Benchmark*'
         OR s.name GLOB 'Fuzz*' OR s.name GLOB 'Example*')
),
transitive_tests AS (
  SELECT ts.id, ts.name, ts.kind, ts.file_path, ts.file_path_rel,
         ts.pkg_path, ts.line_start, ts.col_start, ts.line_end, ts.col_end,
         ts.signature, ts.doc, ts.receiver, f.hash,
         2 AS hop
  FROM call_graph cg1
  JOIN call_graph cg2 ON cg2.callee_id = cg1.caller_id
  JOIN symbols ts ON ts.id = cg2.caller_id
  LEFT JOIN files f ON ts.file_path = f.path
  WHERE cg1.callee_id = :symbol_id
    AND ts.file_path GLOB '*_test.go'
    AND (ts.name GLOB 'Test*' OR ts.name GLOB 'Benchmark*'
         OR ts.name GLOB 'Fuzz*' OR ts.name GLOB 'Example*')
    AND ts.id NOT IN (SELECT id FROM direct_tests)
)
SELECT * FROM direct_tests
UNION ALL
SELECT * FROM transitive_tests
ORDER BY hop, file_path, name
LIMIT :limit OFFSET :offset
```

CTEs ensure each test appears exactly once with its minimum hop count. No ambiguity about deduplication.

## Architecture

### Query layer: `internal/query/tests.go`

```go
// FindTests returns test functions that exercise the given symbol.
// When direct=true, only 1-hop callers are returned.
// Returns []CallRow for consistency with FindCallers/FindCallees — the caller
// side of each CallRow is the test function, the callee side is the target symbol
// (or intermediary for 2-hop). Hop count is communicated via hints in the cmd layer.
func FindTests(db *sql.DB, symbolID string, direct bool, limit, offset int) ([]TestRow, error)
```

`TestRow` wraps `SymbolRow` fields (same columns as the query above) plus `Hop int`. This is a new type because `CallRow` carries both caller and callee fields — tests only need the test function (caller side). The conversion method:

```go
func (r TestRow) ToResult() output.Result  // Analogous to SymbolRow.ToResult()
```

### CLI: `cmd/tests.go`

Follows existing command pattern (mirrors `cmd/callers.go`):

1. Parse args: symbol name, hex ID auto-detect, or `--at` position (via `ResolveSymbolAtPosition`)
2. Resolve to `symbolID` — handle not-found and ambiguous with standard error envelopes
3. Call `query.FindTests(db, symbolID, direct, limit, offset)`
4. Convert to `[]output.Result` via `TestRow.ToResult()`
5. Add hints: `"direct_test"` or `"transitive_test"` based on `Hop`
6. Body enrichment if `--with-body`: use `BatchLookupByID` to avoid N+1 queries
7. `ScoreAndSort`, `ApplySelection`, `TruncateToTokenBudget`
8. Build `output.Response[output.Result]` with standard `Meta`
9. Summary mode: `output.BuildSummary` if `--format=summary`

`--direct` flag controls the `direct` parameter.

### Hints

Each result gets a hint indicating discovery path:

- `"direct_test"` — test directly calls the symbol
- `"transitive_test"` — test calls a helper that calls the symbol

### Sorting

1. Direct tests first (hop=1), then transitive (hop=2)
2. Within each group: same-package tests first (compare `pkg_path` of test symbol against target symbol's `pkg_path`)
3. Then alphabetical by name

### Suggestions

Follow the `output.Suggestion` struct pattern:

```go
func SuggestionsForTests(symbol string, resultCount int) []Suggestion {
    suggestions := []Suggestion{
        {Command: "snipe def " + symbol, Description: "View the function definition", Priority: 1},
        {Command: "snipe callers " + symbol, Description: "See all callers, not just tests", Priority: 2},
    }
    // If many results, suggest summary
    if resultCount > 10 {
        suggestions = append(suggestions, Suggestion{
            Command: "snipe tests " + symbol + " --format=summary",
            Description: "Condensed overview of test coverage",
            Priority: 3,
        })
    }
    return suggestions
}
```

## Zero-coverage signal

When no tests found:

- `results: []` (empty)
- `suggestions`: actionable commands plus a descriptive suggestion:

```go
[]Suggestion{
    {Command: "snipe refs " + symbol, Description: "Check if the symbol is referenced anywhere", Priority: 1},
    {Description: "No tests found for " + symbol + ". Consider adding tests in " + suggestedFile, Priority: 2,
     Condition: "zero_test_coverage"},
}
```

Suggested file derived from symbol's own file path (`strings.TrimSuffix(file, ".go") + "_test.go"`).

## Fallback: ref-based (degraded mode)

If `call_graph` is empty (index built without call graph data):

- `meta.degraded: ["no_call_graph"]`
- Fall back to ref-based search:

```sql
-- Find refs to the symbol in test files, then walk up to enclosing test function
WITH test_refs AS (
  SELECT r.enclosing_id
  FROM refs r
  WHERE r.symbol_id = :symbol_id
    AND r.file_path GLOB '*_test.go'
    AND r.enclosing_id IS NOT NULL
)
SELECT DISTINCT s.id, s.name, s.kind, s.file_path, s.file_path_rel,
       s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
       s.signature, s.doc, s.receiver, f.hash
FROM test_refs tr
JOIN symbols s ON s.id = tr.enclosing_id
LEFT JOIN files f ON s.file_path = f.path
WHERE (s.name GLOB 'Test*' OR s.name GLOB 'Benchmark*'
       OR s.name GLOB 'Fuzz*' OR s.name GLOB 'Example*')

UNION

-- If enclosing is a helper (not Test*), walk one more level
SELECT DISTINCT s2.id, s2.name, s2.kind, s2.file_path, s2.file_path_rel,
       s2.pkg_path, s2.line_start, s2.col_start, s2.line_end, s2.col_end,
       s2.signature, s2.doc, s2.receiver, f2.hash
FROM test_refs tr
JOIN symbols enc ON enc.id = tr.enclosing_id
JOIN call_graph cg ON cg.callee_id = enc.id
JOIN symbols s2 ON s2.id = cg.caller_id
LEFT JOIN files f2 ON s2.file_path = f2.path
WHERE s2.file_path GLOB '*_test.go'
  AND (s2.name GLOB 'Test*' OR s2.name GLOB 'Benchmark*'
       OR s2.name GLOB 'Fuzz*' OR s2.name GLOB 'Example*')
ORDER BY file_path, name
LIMIT :limit OFFSET :offset
```

Note: the second half of the fallback UNION still uses `call_graph` — if call_graph is truly empty, only the first half (direct enclosing) produces results. This is less precise but still useful for the common case where tests directly reference the symbol.

## Known Limitations

- **Interface dispatch**: Static call graph analysis cannot resolve `iface.Method()` to concrete implementations. If a test exercises a symbol only through an interface, it won't be found. The ref-based fallback partially mitigates this since it looks for any mention of the symbol name.
- **Reflection and code generation**: `reflect.ValueOf(x).MethodByName("Foo")` and generated test harnesses are invisible to static analysis.
- **3+ hop chains**: Rare but possible — `TestFoo → setup → middleware → TargetFunc`. 2-hop covers the vast majority of Go test patterns. Can be extended later if telemetry shows gaps.

## Acceptance Criteria

From #108:

- `snipe tests MyFunc` returns test functions that call MyFunc (direct + transitive)
- Works for both same-package and `_test` package tests (external test packages)
- `--direct` restricts to 1-hop only
- `--at file:L:C` works via existing position resolution
- Flags symbols with zero test coverage (empty results + suggestion with file path)
- Degraded mode works when call_graph is empty
- All four Go test types detected: `Test*`, `Benchmark*`, `Fuzz*`, `Example*`
- `--format=summary` supported
- Standard output envelope with hints distinguishing direct vs transitive
- `mage` passes (build + lint + test)

## Implementation Order

1. `internal/query/tests.go` — `TestRow` type + `FindTests` + `FindTestsFallback`
2. `internal/output/types.go` — `SuggestionsForTests`
3. `cmd/tests.go` — command wiring, follows `cmd/callers.go` pattern
4. Tests: unit test for query layer with test fixtures, blackbox test in `test/blackbox/`
5. Verify: `mage qa`
