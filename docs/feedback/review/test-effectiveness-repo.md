# test-effectiveness — repo

scope: project · mode: report · date: 2026-05-17 · run-id: ecebe5258308

## Summary

Test suite is healthy. 81 test files, ~zero mock usage (no `mock.On`, `gomock.EXPECT`, `AssertCalled` matches outside vendor), no tests without assertions, no whole-struct equality of god structs, no `package X` shims for back-door access on the symbols I sampled. Findings below are minor evergreens and a couple of constructor lock-in tests that test the literal, not behavior.

Tier: P1 evergreen 🟢 (3, all minor) · P1 over-broad 🟢 (0) · P1 mock-misuse 🟢 (none) · P2 exported-for-test 🟢 (0 found) · P2 no-assertion 🟢 (0).

## Findings

### 1. evergreen — `require.NotNil(t, fn)` after AST decl loop

- Test: `analyze.TestCheckDocStatus`, `analyze.TestExtractPurpose`, `analyze.TestGeneratePurposeTemplate`
- File: `internal/analyze/docstatus_test.go:74`, `:149`, `:237`
- Issue: `evergreen`
- Code:
  ```go
  f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
  require.NoError(t, err)
  var fn *ast.FuncDecl
  for _, decl := range f.Decls {
      if fd, ok := decl.(*ast.FuncDecl); ok {
          fn = fd; break
      }
  }
  require.NotNil(t, fn)
  ```
- Regression it can't catch: every fixture in these tables embeds at least one `func` decl, and `parser.ParseFile` already succeeded — `fn` cannot be nil unless a future contributor removes the `func` from a fixture, which is a fixture bug, not a SUT regression.
- Fix: drop the three `require.NotNil(t, fn)` lines. The subsequent call (`CheckDocStatus(fn, ...)`, `ExtractPurpose(fn, ...)`, `generatePurposeTemplate(fn)`) will panic-fail loudly if a fixture ever lacks a func decl — same diagnostic value, no fake coverage.

### 2. evergreen — constructor literal lock-in

- Test: `output.TestNewIndexInProgressError`
- File: `internal/output/types_test.go:96`
- Issue: `evergreen`
- Code:
  ```go
  err := NewIndexInProgressError()
  if err.Code != ErrIndexInProgress {
      t.Errorf("Code = %q, want %q", err.Code, ErrIndexInProgress)
  }
  if err.Next == nil { t.Fatal("Next should not be nil ...") }
  if err.Next.Description == "" { t.Error("Next description should not be empty") }
  ```
  `NewIndexInProgressError` is a five-line constructor that returns a literal with `Code: ErrIndexInProgress`, a non-nil `Next`, and a hardcoded `Description`.
- Regression it can't catch: any caller-side regression in how the error is *used* (rendered, routed, surfaced through CLI exit codes). All this test catches is "did someone delete fields from the literal" — which is a refactor signal, not a behavior bug.
- Fix: either delete and rely on `TestErrorMarshal` (already covers `NewMissingIndexError` end-to-end through JSON), or convert this into a CLI-exit-code test that runs `snipe def Foo` against a missing-index repo and asserts the `code` field in actual JSON output.

### 3. evergreen — redundant `require.NotEmpty(t, m.ID)` after slice equality

- Test: `query.TestListMethodsByType` (happy path case)
- File: `internal/query/types_methods_test.go:61`
- Issue: `evergreen`
- Code:
  ```go
  want: []query.MethodInfo{
      {ID: "1", Name: "Alpha", ...},
      {ID: "2", Name: "Beta", ...},
  },
  inspect: func(t *testing.T, got []query.MethodInfo) {
      for _, m := range got {
          require.NotEmpty(t, m.ID)
          require.Contains(t, []string{"(Widget)", "(*Widget)"}, m.Receiver)
          require.Regexp(t, "^[A-Z]", m.Name)
      }
  },
  ```
- Regression it can't catch: nothing — the table's outer `assert.Equal(want, got)` (the runner's default comparison) already pins ID/Receiver/Name to exact values. The `inspect` loop's per-row assertions are looser checks on values that have already been verified strictly.
- Fix: drop this `inspect` block for the happy-path row (keep the empty-result and zero-value-row inspects, which verify orthogonal post-conditions like `require.Empty(got[0].Doc)`). Or, if a property-based check is intended ("every returned receiver matches the queried type"), move it out of the table and run it against the full result set in a dedicated invariant test where it's not shadowed.

## Process notes

- Pre-work probes (`rg 'mock\.|gomock\.|EXPECT|AssertCalled'`) found zero hits outside testify vendor. Mock-misuse tier is genuinely clean.
- Whole-struct `reflect.DeepEqual` is used in `internal/graphmetrics/{graph,scc}_test.go` and `internal/query/boundary_test.go`, but the compared types are small value-only structs (`[]string`, `[][]string`, narrow result rows) — not the over-broad pattern the rule targets.
- Many "`require.NoError` after a call that always returns nil" candidates dissolved on inspection: `parser.ParseFile`, `store.Open`, `sql.Open` all have real failure modes (bad input, fs errors).
- No exported-for-test smell surfaced on a manual sweep of `output`, `store`, `query`, `analyze`. `package edit_test` (external) and `package store` (internal-tests-in-package, used for `removeLockIfContentsMatch`) are both deliberate and appropriate.
- Findings cap requested 10; only 3 produced, by design — fabricating more would dilute the report.

## After

```
trixi log-skill test-effectiveness findings 3 --run-id "ecebe5258308"
```
