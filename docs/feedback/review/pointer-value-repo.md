# Pointer-value review — snipe

Scope: project (non-vendor, non-test). Run: ecebe5258308.

**Verdict:** mostly clean. The codebase consistently uses `*T` for the right reasons —
nil-as-absent (`detectConventions` family, `FindSymbolAtPosition`, `*CommandTable`,
`*FuncAnalysis`/`*Enclosing` omitempty fields), pointer-receiver mutation (`Store`,
`Session`, `Analyzer`, `Comparison.addCheck`, `ChangeResult` build), and error types
(`*output.Error`). **Zero `[]*T` of small elements** in non-vendor code (P2 ptr-slice
clean). **Most `New*` constructors return mutating types** (`*Client`, `*Writer`,
`*BatchClient`, `*FileCache`, `*Store`, `*Analyzer`) — legitimate.

Two real findings below; the rest of the surface passed the per-finding gate.

---

## 1. `small-by-pointer` + `ctor-returns-pointer-no-mutation` — `query.PositionQuery`

- **Site:** `query.ParsePosition` and `query.ResolvePosition` — `internal/query/position.go:12,19,76`
- **Type:** `PositionQuery{File string; Line, Col int}` ≈ **32 bytes**
- **Issue:** `ParsePosition` returns `(*PositionQuery, error)`; `ResolvePosition` takes
  `pos *PositionQuery`. The body never mutates through `pos` (read-only field access
  in five SQL queries). The pointer is not nil-meaningful — error already signals
  parse failure, and `ResolvePosition` would crash on nil regardless.
- **Mutation/nil-use:** none. No caller in `cmd/*.go` (9 call sites: def, refs, sym,
  pack, tests, types, explain, impact, lifecycle) checks `pos == nil` — they always
  go through the `(pos, err) := ParsePosition(at); if err != nil { ... }` idiom.
- **Fix:** return value, pass value.
  ```go
  func ParsePosition(s string) (PositionQuery, error)
  func ResolvePosition(db *sql.DB, pos PositionQuery) (string, error)
  ```
  Drops one heap alloc per query-with-position. Reads cleaner at every call site
  (no `*pos` deref noise; literal `PositionQuery{File: f, Line: l, Col: 1}` works
  directly in tests).

---

## 2. `ctor-returns-pointer-no-mutation` — `index.Fingerprint` (borderline)

- **Site:** `index.ComputeFingerprint`, `computeCombinedHash`, `(*Fingerprint).String` —
  `internal/index/fingerprint.go:13,23,69,85`
- **Type:** `Fingerprint{Version, GoMod, GoSum, GoWork, GoEnv, Combined string}` —
  6 strings ≈ **96 bytes**. At the upper edge of the 64-byte heuristic but still
  small relative to allocation overhead.
- **Issue:** Constructor builds via pointer mutation (`fp := &Fingerprint{...}; fp.GoMod = h; ...`)
  then returns `*Fingerprint`. After construction the value is read-only:
  `computeCombinedHash(fp *Fingerprint)` and `(fp *Fingerprint) String()` only read.
  No nil-meaningful semantics (error path returns `nil, err` like every Go function;
  callers don't branch on nil-as-data).
- **Mutation/nil-use:** mutation **only during construction**; nil never used as a
  signal.
- **Fix:** build a local value then return.
  ```go
  func ComputeFingerprint(dir, version string) (Fingerprint, error) {
      fp := Fingerprint{Version: version}
      // ... fp.GoMod = h, etc.
      fp.Combined = computeCombinedHash(fp)
      return fp, nil
  }
  func computeCombinedHash(fp Fingerprint) string
  func (fp Fingerprint) String() string
  ```
  Single fingerprint per index — escape impact tiny. Flagged for **consistency
  with the `Coupling` value-type model in `internal/graphmetrics/coupling.go`**
  (same shape: build, hash, no mutation after) rather than allocation cost.
  Lower priority than #1.

---

## Considered, dropped

| Site | Why dropped |
|---|---|
| `extractFuncSymbol/extractTypeSymbol → *Symbol` (`internal/index/symbols.go:93,189`) | nil = "not a symbol" is meaningful; callers check `if sym != nil` before deref. |
| `detect{Constructors,Receivers,Testing,…} → *XConvention` | nil = "no convention detected" → `omitempty` JSON. Real nil-as-absent. |
| `FindSymbolAtPosition / FindEnclosingSymbol → *SymbolRow` | nil = "not found" — callers in `cmd/search.go`, `cmd/impact.go` branch on it. |
| `Apply / ApplyAndWrite → *Result` | error already signals failure, but `Result` has 4 strings + 3 ints + bool (~88B) and the function is called once per edit op — not hot. Idiomatic enough. |
| `Compare → *Comparison` | `addCheck` mutates via pointer receiver. Pointer doing its job. |
| `LoadConfig`, `CaptureConfig`, `CompareConfig`, `GenerateConfig` already passed **by value** | Codebase already does the right thing here. |
| `NewAnalyzer → *Analyzer` | `lines [][]byte` lazily mutated; methods need pointer receiver. |
| `Store`, `Session`, `FileCache`, `Client`, `BatchClient`, `Writer` constructors | All have mutating methods. Idiomatic. |
| All `[]*T` matches | Zero in non-vendor non-test code. P2 clean. |

---

Cap: 10. Real findings emitted: 2 (no padding per gate).
