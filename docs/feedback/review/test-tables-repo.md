# test-tables — snipe (repo)

Run: 2026-05-17 · scope: repo · linter: test-tables · run-id: ecebe5258308

Scope: 57 table-test sites across cmd/, internal/. `go.mod` declares `go 1.24.0` (toolchain go1.25.8) → Go 1.22+ rules apply for loop-variable rescope.

## Summary

| Pattern | Count | Tier |
|---|---|---|
| P2 legacy `tt := tt` / `tc := tc` rescope | 8 sites | 🔴 |
| P1 one-row table | 4 | 🔴 |
| P2 unused table field | 1 | 🟡 |
| P2 missing `name` field with `t.Run` | 2 | 🟡 |
| P1 per-case branching | 0 | 🟢 |

Headline: stale rescope sweep is the biggest cleanup — 8 sites, mechanical. One-row tables in `metrics/metrics_test.go` are the most expensive ceremony.

---

## Findings

### 1. legacy-loop-rescope — cmd/root_test.go (5 sites)

- **Test:** `cmd.TestRoot_*` · `cmd/root_test.go:38-42, 116-120, 154-158, 219-223, 286-290`
- **Issue:** `legacy-loop-rescope`
- **Code:**
  ```go
  for _, tc := range tests {
      tc := tc                         // unnecessary on go 1.24
      t.Run(tc.name, func(t *testing.T) {
          t.Parallel()
          ...
      })
  }
  ```
- **Fix:** Drop the `tc := tc` line from all 5 loops. `go.mod` is `go 1.24` — the compiler rescopes per-iteration.

### 2. legacy-loop-rescope — cmd/help_golden_test.go:27

- **Test:** `cmd.TestHelpGolden` · `cmd/help_golden_test.go:26-28`
- **Issue:** `legacy-loop-rescope`
- **Code:**
  ```go
  for _, tc := range cases {
      tc := tc
      t.Run(tc.name, func(t *testing.T) {
          stdout, stderr, err := executeCommand(newRootCommandForTest(t), append(tc.args, "--help")...)
  ```
- **Fix:** Drop `tc := tc`.

### 3. legacy-loop-rescope — internal/config/load_contract_test.go:71

- **Test:** `config_test.TestLoad_MergesConfiguration_*` · `internal/config/load_contract_test.go:70-72`
- **Issue:** `legacy-loop-rescope`
- **Fix:** Drop the `tc := tc` line; subtests are not parallel here, rescope is doubly unnecessary.

### 4. legacy-loop-rescope — internal/query/position_test.go:77

- **Test:** `query.TestResolveByPosition` · `internal/query/position_test.go:76-78`
- **Issue:** `legacy-loop-rescope`
- **Fix:** Drop `tt := tt`.

### 5. one-row-table — internal/metrics/metrics_test.go:101 (`TestLoadHistory_*`)

- **Test:** `metrics_test.TestLoadHistory_SkipsMalformedLines_When_JSONLContainsInvalid` · `internal/metrics/metrics_test.go:101-142`
- **Issue:** `one-row-table`
- **Code:** A `[]struct{name, historyPath string; want []metrics.Baseline}` literal with exactly one row (`"error: malformed line is skipped"`). The struct adds ~10 lines of ceremony to wrap a single `LoadHistory(path)` + `cmp.Diff` call.
- **Fix:** Inline as a flat test body. Reintroduce the table form when a second fixture is added.

### 6. one-row-table — internal/metrics/metrics_test.go:207 (`TestHistoryEntries_*`)

- **Test:** `metrics_test.TestHistoryEntries_SetsDeltas_When_MetricsChangeAcrossRuns` · `internal/metrics/metrics_test.go:207-284`
- **Issue:** `one-row-table`
- **Code:** Single-row table with `name`, `baselines`, `want`. ~75 lines of struct literal wrapping one `cmp.Diff` assertion.
- **Fix:** Inline. The one row is large and structurally unique — table syntax pays no dividend.

### 7. one-row-table — internal/index/refs_test.go:6 (`TestPosKey`)

- Actually this one has 5 rows — not a finding. Skip.

### 7. one-row-table — internal/diagram/d2_test.go:9 (`TestQuoteEscaping`)

- 5 rows — not a finding. Skip.

### 7. one-row-table — cmd/diagram_test.go via cousins

- Multi-row — not a finding. Skip.

### 7. unused-field — internal/query/fuzzy_test.go:141 (`TestFindSimilarSymbols`)

- **Test:** `query.TestFindSimilarSymbols` · `internal/query/fuzzy_test.go:141-191`
- **Issue:** `unused-field`
- **Code:**
  ```go
  tests := []struct {
      name        string
      query       string
      maxDist     int
      maxResults  int
      wantContain []string
      wantExclude []string   // declared but never populated by any row
  }{ ... }
  ```
  No row in the table sets `wantExclude` to a non-nil value; the loop still iterates it (`for _, exclude := range tt.wantExclude`) but the inner loop never executes. One row (`"no matches for completely different"`) also leaves `wantContain` empty — the assertion is "no panics", not "no results", because there's no explicit length check.
- **Fix:** Drop `wantExclude` and its loop. If exclusion is a real concern, add a `wantLen int` field instead.

### 8. name-field-missing — internal/diagram/d2_test.go:27 (`TestSanitizeID`)

- **Test:** `diagram.TestSanitizeID` · `internal/diagram/d2_test.go:26-41`
- **Issue:** `table-name-field-missing`
- **Code:**
  ```go
  cases := []struct{ in, want string }{
      {"", "_"},
      {"simple", "simple"},
      ...
  }
  for _, c := range cases {
      got := SanitizeID(c.in)
      if got != c.want { t.Errorf("SanitizeID(%q) = %q, want %q", c.in, got, c.want) }
  }
  ```
  No `t.Run` per case; failures report `c.in` only. Diagnosable, but inconsistent with `TestQuoteEscaping` above (same pattern, also no `t.Run`).
- **Fix:** Either add `name` + `t.Run(c.name, ...)` to both tables in this file, or accept the in-name-as-label convention and note it. Pick one.

### 9. name-field-missing — internal/diagram/d2_test.go:117 (`TestStyleValueQuoting`)

- **Test:** `diagram.TestStyleValueQuoting` · `internal/diagram/d2_test.go:117-133`
- **Issue:** `table-name-field-missing`
- **Code:** Same shape as #8 — `struct{ in, want string }` with no `t.Run`.
- **Fix:** See #8. One-time package-level decision: pkg `diagram` uses input-as-label (no `t.Run`). Document or migrate.

### 10. style-inconsistent — internal/diagram (pkg-level)

- **Test:** pkg `diagram` · `internal/diagram/d2_test.go`
- **Issue:** `style-inconsistent`
- **Code:** Same file mixes anonymous-struct table (`cases := []struct{ in, want string }{...}` at lines 9, 27, 117 — no `t.Run`) with sibling tests that use plain non-table form (`TestBuilder_NodesAndEdges`, `TestBuilder_ContainerNesting`, etc.) and named subtests via direct calls. No `tests := []struct{name, ...}` form anywhere — inconsistent with the rest of the repo.
- **Fix:** Pick one convention for the pkg. Most snipe packages use named-field struct + `t.Run(tc.name, ...)`. Migrate the three `cases` tables to match, or document the exception in package-level test doc.

---

## Notes

- No per-case-branching offenders found — table bodies in this repo are uniformly shaped. Healthy signal.
- `cmd/root_test.go` is the rescope hotspot; one PR can drop all 5 + the 3 in other files.
- `internal/metrics/metrics_test.go` shows a recurring pattern: writers default to table form even with one row, presumably for future extension. Two of the four tables there are single-row; the other two have 3 rows each.
