# Staff Engineering Assessment: Query/Index Core

This review focuses on the deterministic query pipeline, index-backed data access, scoring/truncation behavior, and degradation/freshness contracts.

## 1) Mental model of the query pipeline

### A. Lookup (name / position / fuzzy)

1. **Position-first resolution (`def --at`, `refs --at`)**
   - `query.ResolvePosition` uses a staged fallback pipeline:
     1) exact symbol identifier position (`symbols.name_line/name_col`),
     2) exact reference position (`refs.line/col`),
     3) nearest symbol on same line,
     4) nearest ref on same line,
     5) containing symbol on same line/col,
     6) smallest enclosing symbol span.
   - This is deterministic due to explicit ordering and deterministic SQL `ORDER BY` tie breaks.
   - Primary location: `internal/query/position.go` (`ResolvePosition`).

2. **Name lookup (`def <name>`, `refs <name>`)**
   - `query.LookupByName` dispatches by syntax:
     - method syntax (`(*T).M`, `T.M`) → `lookupMethod`,
     - qualified (`pkg/path.S`) → `lookupQualified`,
     - simple name → `lookupSimple` with exact-match first and case-insensitive fallback on miss.
   - Ambiguity is surfaced intentionally (candidates returned) rather than guessed.
   - Primary locations: `internal/query/lookup.go` (`LookupByName`, `lookupSimple`, `lookupQualified`, `lookupMethod`), `cmd/def.go`, `cmd/refs.go`.

3. **Fuzzy fallback**
   - When no exact name match exists, `def` uses Levenshtein-based suggestions (`FindSimilarSymbols` + `DefaultMaxDistance`).
   - Candidate pool is bounded (`LIMIT 1000` then fallback `LIMIT 5000`) to bound latency.
   - Primary location: `internal/query/fuzzy.go`, call site in `cmd/def.go`.

### B. Refs / calls / imports

1. **Refs**
   - `FindRefs` resolves references by `symbol_id`, joins enclosing symbol metadata and file hash metadata, and applies deterministic ordering (`file_path`, `line`).
   - `cmd/refs.go` adds optional post-filters (`kind`, `file`, `pkg`), supports body/context expansion, and uses batch lookup for enclosing symbols to avoid per-row lookup when `--with-body` is requested.

2. **Calls (callers/callees)**
   - Call edges are persisted in `call_graph` and queried by `callee_id`/`caller_id` joins to `symbols`.
   - There are explicit type-aggregated variants using receiver-subqueries (`FindCallersForType`, `FindCalleesForType`) to avoid round-trips.
   - Primary location: `internal/query/lookup.go` (`FindCallers`, `FindCallees`, `FindCallersForType`, `FindCalleesForType`).

3. **Imports**
   - Import queries are straightforward scans with package/file predicates (`FindImports*`).
   - Some patterns are suffix/contains based (`pkg_path = ? OR pkg_path LIKE '%/'+?`, directory `%...%`) and may degrade index selectivity.
   - Primary location: `internal/query/imports.go`.

### C. Scoring and truncation

1. **Selection and ranking**
   - Results are scored via explicit heuristics (`ScoreResult`) and then stable-sorted with deterministic tiebreakers (`File`, `Name`).
   - Global `--select` mode (`best`/`top3`/`top5`/`all`) truncates by count after scoring.
   - Primary location: `internal/output/json.go` (`ScoreResult`, `SortByScore`, `ScoreAndSort`), `cmd/root.go` (`ApplySelection`).

2. **Token-budget handling**
   - `TruncateToTokenBudget` performs whole-result truncation using estimated tokens with fixed overhead reserve.
   - There is also a semantic truncator (`TruncateResultsSemantic`) that truncates bodies at statement boundaries before dropping whole results, but current command usage is inconsistent.
   - Primary location: `internal/output/json.go`.

### D. Index freshness and degradation

1. **Freshness**
   - `CheckIndexState` compares computed fingerprint (repo + tool version) against stored `meta.fingerprint` and emits `fresh|stale|missing`.
   - `CheckFileStaleness` checks per-file mtime drift for result files.
   - Primary location: `internal/query/state.go`.

2. **Degradation reporting**
   - Commands emit explicit `degraded[]` reasons for non-fatal quality drops (`body_extraction_failed`, `context_extraction_failed`, `batch_lookup_failed`, `no_index`, etc.).
   - This is a strong contract for LLM consumers because it distinguishes “empty because none” from “empty because degraded path”.
   - Primary locations: `cmd/def.go`, `cmd/refs.go`, `cmd/search.go`.

---

## 2) Ranked recommendations (10–15)

### 1. Add and verify covering indexes for hottest lookup/order pairs
- **What to change**: Add/validate composite indexes aligned with hot predicates + orderings used in lookup and call/ref traversals, especially for `ORDER BY ... LIMIT ...` query shapes.
- **Where**: `internal/store/schema.go` migrations; target queries in `internal/query/position.go`, `internal/query/lookup.go`, `internal/query/imports.go`.
- **Why**: Improves bounded latency under larger corpora, reduces temp B-tree sorts.
- **Effort/Risk**: Medium effort, low correctness risk.
- **How to verify**: Add query-plan tests using `EXPLAIN QUERY PLAN`; benchmark p50/p95 for def/refs/callers on fixture DB.

### 2. Replace leading-wildcard predicates on large tables with anchored alternatives where possible
- **What to change**: Avoid `LIKE '%...%'` on `pkg_path`/file paths for primary interactive paths; prefer exact, suffix-normalized, or precomputed reversed-path helper column when necessary.
- **Where**: `lookupQualified` and package filters in `internal/query/lookup.go`; importers in `internal/query/imports.go`.
- **Why**: Leading wildcard defeats index usage and creates table/index scans.
- **Effort/Risk**: Medium effort, medium risk (behavioral semantics can shift).
- **How to verify**: Plan diff (`EXPLAIN QUERY PLAN`) + regression tests for short package names and suffix patterns.

### 3. Deduplicate/normalize candidate IDs before batch lookup and add hard caps
- **What to change**: Normalize IDs (set semantics) before `BatchLookupByID`, enforce a command-level cap to avoid oversized `IN (...)` lists.
- **Where**: `cmd/refs.go` and `internal/query/lookup.go` (`BatchLookupByID`).
- **Why**: Better memory/latency predictability and lower SQL parse overhead.
- **Effort/Risk**: Low effort, low risk.
- **How to verify**: Unit test duplicate-heavy refs payload and large payload cap behavior.

### 4. Standardize truncation strategy across commands on semantic truncation path
- **What to change**: Prefer `TruncateResultsSemantic` for body-heavy commands (`def`, `refs`, `callers`, `callees`, `show`) with consistent overhead policy.
- **Where**: command handlers + `internal/output/json.go`.
- **Why**: Better token-budget utilization and UX without violating deterministic behavior.
- **Effort/Risk**: Medium effort, low risk.
- **How to verify**: Snapshot/golden tests for truncation shape and `meta.truncated` behavior.

### 5. Promote staleness checks from mtime-only to hash-first for returned files
- **What to change**: If file hash available in `files.hash`, compare hash for result files before/alongside mtime.
- **Where**: `internal/query/state.go` + data already surfaced in `SymbolRow`/`RefRow`.
- **Why**: mtime can be noisy or stale depending on filesystem behavior; hash improves correctness signaling.
- **Effort/Risk**: Medium effort, low risk.
- **How to verify**: Unit tests where mtime unchanged but content changed (or vice versa).

### 6. Add explicit context propagation to query functions
- **What to change**: Thread `context.Context` through query APIs and DB calls for cancellation and latency bounding.
- **Where**: TODO already documented in `internal/query/lookup.go`; propagate through command handlers.
- **Why**: Stronger bounded latency guarantees under heavy loads and cancellation-aware callers.
- **Effort/Risk**: Medium-high effort, medium risk (signature churn).
- **How to verify**: Cancellation tests with forced timeouts and ensured early return.

### 7. Reduce accidental heuristics in package resolution by surfacing confidence
- **What to change**: `ResolvePkgPattern` should return `(resolved, confidence)` and commands should expose degradation/ambiguity metadata when short-name resolution is non-unique.
- **Where**: `internal/query/resolve.go`, command handlers consuming `--pkg`.
- **Why**: Current “shortest path wins” is deterministic but can be semantically wrong in mono-repos.
- **Effort/Risk**: Medium effort, medium risk.
- **How to verify**: Tests with multiple `.../store` packages and expected ambiguity metadata.

### 8. Make fuzzy search budget-aware and index-selective
- **What to change**: Constrain fuzzy candidate set by length window and/or prefix buckets before Levenshtein; keep hard deterministic limits.
- **Where**: `internal/query/fuzzy.go`.
- **Why**: Current secondary pass over up to 5000 names has O(k * |q| * |name|) cost and can dominate miss latency.
- **Effort/Risk**: Low-medium effort, low risk.
- **How to verify**: Microbenchmarks on symbol dictionaries with worst-case misses.

### 9. Precompute/maintain receiver-normalized columns for method/type queries
- **What to change**: Store normalized receiver type (no parens/pointer marker split fields) to avoid string operations in SQL and simplify type/call aggregation queries.
- **Where**: schema + index writer (`internal/store/write.go`) + consumers (`GetMethodsForType`, type caller/callee queries).
- **Why**: Cleaner SQL, fewer string-transform edge cases, better indexability.
- **Effort/Risk**: Medium effort, medium risk (migration + backfill).
- **How to verify**: Tests for `T` and `*T` method coverage across packages.

### 10. Tighten imports query semantics to avoid accidental matches
- **What to change**: Replace broad `%dir%` matching in `FindImportersByDirectory` with anchored package-path boundary matching.
- **Where**: `internal/query/imports.go`.
- **Why**: Current behavior may overmatch unrelated packages and inflate noisy results.
- **Effort/Risk**: Low effort, low-medium UX risk (fewer but cleaner matches).
- **How to verify**: Unit tests with confusable package names (`internal/handler` vs `internal/handler2`).

### 11. Add deterministic tie-breaks wherever absent in ordered SQL
- **What to change**: Ensure all `ORDER BY` queries include stable secondary keys (`id`, `name`, line/col) when equivalent rows exist.
- **Where**: selected queries in `internal/query/lookup.go`, `internal/query/imports.go`.
- **Why**: Strengthens predictability contract across SQLite versions/plans.
- **Effort/Risk**: Low effort, low risk.
- **How to verify**: Golden tests under randomized insertion order.

### 12. Add explicit SLO-style benchmark gates for core queries
- **What to change**: Add benchmark harness for `def/refs/callers/callees/search-enriched` at varying corpus sizes and ambiguity levels.
- **Where**: `test/bench/*`.
- **Why**: Makes bounded-latency a testable invariant rather than an aspiration.
- **Effort/Risk**: Medium effort, low risk.
- **How to verify**: CI benchmark thresholds (warn/fail on regression).

---

## 3) What is well-chosen vs where debt is accumulating

### Well-chosen, preserve these

1. **Deterministic fallback ladder in `ResolvePosition`**
   - Explicit, ordered stages with deterministic tie-breaks are excellent for LLM reliability.

2. **“Exact first, fallback second” strategy in lookup**
   - `lookupSimple` exact match before case-insensitive fallback preserves speed and precision on the common path.

3. **Explicit degraded metadata in responses**
   - Strong operator ergonomics for downstream agents; keeps failures legible without hard-failing useful output.

### Debt accumulating (correctness/performance)

1. **Wildcard-heavy package/path matching**
   - Multiple `LIKE '%...%'` patterns risk index bypass and noisy matches as corpus grows.

2. **Heuristic package short-name resolution without ambiguity signaling**
   - Deterministic but not always correct; hidden uncertainty can mislead consumers.

3. **Inconsistent token truncation policy across commands**
   - Multiple truncation paths exist; semantic truncation utility is underused where it matters most.

---

## 4) Two small, concrete patches to implement first

### Patch A (low risk, high value): dedupe and cap batch lookup IDs
- **Change**: In `cmd/refs.go`, deduplicate `enclosingIDs` before calling `BatchLookupByID`, and enforce a fixed deterministic cap (e.g., first 1000 sorted unique IDs).
- **Expected impact**: Better tail latency and lower memory/SQL parse overhead for high-reference symbols.
- **Verification**: unit test for duplicate-heavy refs input + benchmark comparing current vs deduped behavior.

### Patch B (low-medium risk): anchor directory importer matching
- **Change**: In `FindImportersByDirectory`, replace `%dir%` with boundary-aware matching semantics to avoid substring collisions.
- **Expected impact**: Better correctness (fewer false positives) and potentially better index selectivity.
- **Verification**: targeted tests with lookalike package names and golden output diff.

---

## Complexity notes (selected)

- **Levenshtein fuzzy step**: O(k * |q| * |name|), with current bounded candidate set (`k <= 5000` in broad pass). This is acceptable for misses today but sensitive to large symbol dictionaries.
- **Position resolution path**: each SQL stage is O(log N) if indexed as intended, but nearest-column queries (`ORDER BY ABS(...)`) may require scan of same-line subset.
- **Call/ref traversals**: mostly index-friendly joins keyed by `caller_id`/`callee_id`/`symbol_id`, with deterministic ordering and pagination.

Overall: The system is fundamentally well-organized for deterministic LLM tooling. The main opportunities are to tighten index-selective predicates, make ambiguity/degradation even more explicit, and unify budget-aware truncation semantics.
