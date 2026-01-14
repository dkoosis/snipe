# snipe repository review (excellence + performance)

## 1) Executive summary (10 lines max)
- **Overall:** Strong MVP with clear output schema and structured indexing, but several correctness and scaling risks in indexing and query resolution could undermine trust in results.
- **Top strengths:**
  1) Clear CLI + JSON contract that is consistent across commands.
  2) SQLite schema and WAL mode enable fast read queries and deterministic results.
  3) Static indexing approach avoids daemon dependencies and yields predictable latency.
- **Top risks:**
  1) Incorrect symbol position key generation can silently break refs/call graph linkage.
  2) Path matching and LIKE queries reduce correctness and index efficiency at scale.
  3) Streaming/scan logic in `rg` and file reads can truncate results on long lines.

## 2) Ranked recommendations (most important first)

### **Rank #1 — Fix position-key construction to avoid symbol/refs mismatches**
- **Category:** Correctness
- **Impact:** High
- **Confidence:** High
- **Effort:** S
- **Risk:** Low
- **Evidence:** `internal/index/refs.go` `posKey` builds keys using `string(rune(line))` and `string(rune(col))`, which truncates numeric values to runes (e.g., line 300 becomes a single Unicode codepoint). This can create collisions or mismatches between symbols and references.
- **Why it matters:** If refs and call graph edges cannot reliably map to symbols, core navigation commands will return incomplete or incorrect results, violating the determinism contract.
- **Recommendation:** Replace `posKey` with a proper numeric format (e.g., `fmt.Sprintf("%s:%d:%d", file, line, col)` or `strconv.Itoa`).
- **Suggested next step:** Update `posKey` and add a unit test that asserts stable key generation for multi-digit line/col values.

### **Rank #2 — Fix exclusion matching to correctly skip vendor/testdata/etc.**
- **Category:** Performance
- **Impact:** High
- **Confidence:** High
- **Effort:** S
- **Risk:** Low
- **Evidence:** `internal/index/loader.go` `matchesExclude` uses `filepath.SplitList(pkgPath)`, which is meant for PATH lists, not path components. This can fail to match `vendor`, `testdata`, etc., leading to indexing extra packages.
- **Why it matters:** Over-indexing inflates load time, memory, and DB size, and it increases query times. It also increases the chance of ambiguous symbol results.
- **Recommendation:** Split package paths by `/` (or `filepath.Split`/`strings.Split`) and compare components; consider also checking actual file paths for exclusion.
- **Suggested next step:** Replace `filepath.SplitList` with a component-aware split and add a test for exclusion logic.

### **Rank #3 — Avoid `LIKE '%path%'` for position resolution (correctness + perf)**
- **Category:** Correctness
- **Impact:** High
- **Confidence:** Med
- **Effort:** M
- **Risk:** Med
- **Evidence:** `internal/query/position.go` resolves symbols with `file_path LIKE ?` and a leading wildcard. This can match multiple files with the same suffix and bypass the `idx_*_file` index.
- **Why it matters:** Incorrect symbol resolution is extremely costly for navigation (wrong jump target). The leading wildcard also turns queries into scans as the index grows.
- **Recommendation:** Store normalized absolute paths (and optionally repo-relative paths) and use exact matches or suffix-only matches when the input is relative. Add a `file_path_rel` column or compute `repo_root` and rewrite queries as exact or prefix matches.
- **Suggested next step:** Add `file_path_rel` to the schema and update `ResolvePosition` to prefer exact matches before any `LIKE` fallback.

### **Rank #4 — Increase `bufio.Scanner` limits and surface scan errors**
- **Category:** Reliability
- **Impact:** Medium
- **Confidence:** High
- **Effort:** S
- **Risk:** Low
- **Evidence:** `internal/search/rg.go` uses `bufio.Scanner` without increasing buffer size. Long lines (>64K) will error and silently truncate results. `scanner.Err()` is never checked.
- **Why it matters:** Large lines are common in minified JS or generated files. Silent truncation breaks correctness and degrades user trust.
- **Recommendation:** Call `scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)` (or larger) and return an error if `scanner.Err()` is non-nil.
- **Suggested next step:** Add `scanner.Buffer` and check `scanner.Err()` after the loop.

### **Rank #5 — Handle `rg` exit codes and stderr explicitly**
- **Category:** Reliability
- **Impact:** Medium
- **Confidence:** Med
- **Effort:** S
- **Risk:** Low
- **Evidence:** `internal/search/rg.go` ignores `cmd.Wait()` errors, even though `rg` returns non-zero for errors other than “no matches.”
- **Why it matters:** IO errors, invalid regex, or permission issues should be surfaced as structured errors instead of returning partial results.
- **Recommendation:** Call `cmd.Wait()` and treat exit code 1 as “no matches” but propagate other failures. Consider capturing stderr to provide actionable messages.
- **Suggested next step:** Use `exec.ExitError` and inspect `ExitCode()`, only ignore `1`.

### **Rank #6 — Mitigate SQLite “database is locked” risks**
- **Category:** Reliability
- **Impact:** Medium
- **Confidence:** Med
- **Effort:** S
- **Risk:** Low
- **Evidence:** `internal/store/store.go` enables WAL but does not set `busy_timeout`, `synchronous`, or connection pool limits. SQLite can return `database is locked` when commands overlap or read while writing.
- **Why it matters:** The CLI might be invoked concurrently by a toolchain, leading to flaky failures on index creation/read.
- **Recommendation:** Set a reasonable `busy_timeout` and cap connections (e.g., `db.SetMaxOpenConns(1)` for serialized access). Consider `PRAGMA synchronous=NORMAL` for faster writes if acceptable.
- **Suggested next step:** Add `PRAGMA busy_timeout=5000` and set max open conns to 1 in `Open`.

### **Rank #7 — Reduce O(N*M) enclosing lookup for refs/calls**
- **Category:** Performance
- **Impact:** Medium
- **Confidence:** Med
- **Effort:** M
- **Risk:** Med
- **Evidence:** `internal/index/refs.go` and `internal/index/callgraph.go` use `findEnclosing` that linearly scans all functions for each reference/call. This scales poorly for large files.
- **Why it matters:** Reference extraction can dominate index time on large repos. O(N*M) behavior can turn into minutes of CPU.
- **Recommendation:** Use an AST walk that tracks the current enclosing function and assigns it to each ident/call as you walk. Alternatively, sort ranges and binary search.
- **Suggested next step:** Replace `findEnclosing` with a walker that keeps a stack of current function ranges.

### **Rank #8 — Avoid repeated file reads for snippets and handle long lines**
- **Category:** Performance
- **Impact:** Medium
- **Confidence:** Med
- **Effort:** M
- **Risk:** Low
- **Evidence:** `internal/index/refs.go` reads each file with `bufio.Scanner`, which can silently drop long lines and rereads files for each package iteration.
- **Why it matters:** IO and scanning cost is high in large repos; long lines break snippet extraction.
- **Recommendation:** Cache file contents per path during indexing and use `bufio.Reader` to avoid scanner limits. Consider line index slices to reduce allocations.
- **Suggested next step:** Build a `map[string][]string` of lines or a `[]byte` cache per file to reuse across packages.

### **Rank #9 — Tighten symbol signature and receiver formatting**
- **Category:** Maintainability
- **Impact:** Low
- **Confidence:** Med
- **Effort:** S
- **Risk:** Low
- **Evidence:** `internal/index/symbols.go` formats signatures manually and doesn’t include package-qualified types or full receiver type context.
- **Why it matters:** Ambiguous or lossy signatures can mislead both humans and LLMs, especially when multiple packages share type names.
- **Recommendation:** Use `types.TypeString` with a package qualifier to include full paths when possible.
- **Suggested next step:** Add a `types.Qualifier` in `formatFuncSignature` to render fully qualified names when `pkg` is available.

### **Rank #10 — Add schema indexes for position queries**
- **Category:** Performance
- **Impact:** Medium
- **Confidence:** Med
- **Effort:** S
- **Risk:** Low
- **Evidence:** `internal/query/position.go` uses `line` + `col` filtering on `refs` and `symbols`, but the schema has only `idx_refs_file` and no composite index on `(file_path, line)` or `(file_path, line, col)`.
- **Why it matters:** Position lookups are a hot path for `--at` queries; missing composite indexes leads to scans.
- **Recommendation:** Add composite indexes like `refs(file_path, line, col)` and `symbols(file_path, line_start, col_start)`.
- **Suggested next step:** Update `schema.go` and add a migration note in `meta` if schema versioning is used later.

### **Rank #11 — Limit ambiguous qualified-name matches**
- **Category:** Correctness
- **Impact:** Low
- **Confidence:** Med
- **Effort:** M
- **Risk:** Med
- **Evidence:** `internal/query/lookup.go` qualified lookup uses `file_path LIKE %pkgPath%`, which can match unrelated paths containing the same substring.
- **Why it matters:** Qualified lookups should reduce ambiguity, not increase it; false positives lead to `AMBIGUOUS_SYMBOL` errors.
- **Recommendation:** Store `pkg_path` in the symbols table (from `packages.Package.PkgPath`) and query it directly.
- **Suggested next step:** Add `pkg_path` to `symbols`, populate during indexing, and query `WHERE pkg_path = ? AND name = ?`.

### **Rank #12 — Add cancellation/context support for long operations**
- **Category:** DX
- **Impact:** Low
- **Confidence:** Med
- **Effort:** M
- **Risk:** Low
- **Evidence:** Long-running commands (`index`, `search`) do not use context cancellation; `rg` and `go/packages` can run indefinitely on large repos.
- **Why it matters:** Tools integrating `snipe` may need deadlines to keep interactive UX snappy.
- **Recommendation:** Thread `context.Context` through indexing/search and respect it when running `rg` or `packages.Load`.
- **Suggested next step:** Add `context.Background()` in commands, and allow `--timeout` flag to enforce deadlines.

## 3) Performance deep dive (hot paths)

**Likely hot paths identified**
- Indexing path: `index.Load` + `ExtractSymbols` + `ExtractRefs` + `ExtractCallGraph`.
- Query path: `ResolvePosition`, `LookupByName`, `FindRefs`.
- Search path: `rg` subprocess + JSON parsing.

**Allocation and copying risks**
- `ExtractRefs` loads full file contents into `[]string` for every file; repeated allocations across packages could be high. Introduce a file cache or a single-pass per file.
- `formatFuncSignature` builds strings manually; not large but could be improved with a `types.Qualifier` and reuse.

**SQLite usage patterns**
- WAL mode is set, but no `busy_timeout` or connection caps. Under concurrent use, this can cause transient lock errors.
- Position resolution uses `LIKE` and unindexed filters; add composite indexes to keep latency low.

**Subprocess overhead (rg/gopls)**
- `rg` is invoked per search. Consider reusing arguments or pre-checking `rg` availability to avoid repeated lookups.
- `rg` output is streamed and parsed; ensure scanner buffer is large and errors are surfaced to avoid partial output.

**Concurrency hazards**
- No goroutines or worker pools in the indexing pipeline; CPU parallelism is limited. Introducing concurrency could speed indexing but risks reordering or increasing memory use.
- Without connection caps, SQLite may open multiple connections unexpectedly, leading to contention.

**Latency budget suspects**
- `packages.Load` is likely the dominant cost on large repos.
- `ExtractRefs` and `ExtractCallGraph` do per-node operations; the linear `findEnclosing` scan is the biggest avoidable cost.

## 4) “Keep” list
- **Deterministic JSON output schema** in `internal/output` keeps the CLI automation-friendly.
- **Use of WAL mode** in SQLite is a good call for read-heavy workloads.
- **Static indexing via `go/packages`** avoids gopls daemon startup overhead.
- **Command structure with Cobra** keeps CLI extensibility straightforward.
- **Explicit error shapes** (`NOT_FOUND`, `AMBIGUOUS_SYMBOL`, etc.) aid automated consumers.

## 5) Appendix

### Proposed benchmark plan
- **Indexing:** Add benchmarks for `ExtractSymbols`, `ExtractRefs`, `ExtractCallGraph` using a medium-sized synthetic repo (100–500 files). Measure total time and allocations.
- **Queries:** Bench `ResolvePosition` and `LookupByName` against a populated SQLite DB with 50k+ symbols to see index effectiveness.
- **Search:** Bench `search.Search` with a large directory and a pattern with high hit counts to measure parse overhead.

### Proposed profiling plan
- **CPU pprof:** Wrap `index` command to record CPU profiles around `packages.Load`, `ExtractRefs`, and `ExtractCallGraph`.
- **Heap pprof:** Capture during indexing to find line-cache and AST-related allocations.
- **Block profile:** If SQLite lock contention is observed under concurrent usage.

### Quick code hygiene wins
- Add `scanner.Err()` checks for file/rg scanning loops.
- Add a small `go test ./...` target in CI to keep tests visible.
- Add a schema version check and migration stubs in `store` to handle future changes safely.
