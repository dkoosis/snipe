# Go Codebase True-Bug Audit (3 Passes)

Date: 2026-02-26  
Repo: `/workspace/snipe`

## PASS 1 — True Bugs Audit (Correctness & Reliability)

### System Map (concise)
- **Entry points**: Cobra CLI commands are registered under `rootCmd`; key operational paths are `index`, `watch`, and `sim`. (`cmd/root.go`, `cmd/index.go`, `cmd/watch.go`, `cmd/sim.go`)
- **Concurrency model**:
  - Signal-cancellation goroutine in command pre-run (`cmd/root.go`).
  - Long-lived fsnotify loop in `watch` command (`cmd/watch.go`).
  - Streaming multipart writer goroutine for batch upload (`internal/embed/batch.go`).
- **Persistence**: SQLite store via `internal/store` with transactional full and incremental index writes.
- **External calls**: Voyage embedding APIs in `internal/embed/client.go` and `internal/embed/batch.go`.
- **Error handling conventions**: often fail-fast with wrapped errors; some best-effort paths intentionally ignore errors.

### Findings (ranked)

#### 1) Full index rebuild can fail hard with FK constraint and abort indexing
- **Severity**: High
- **Evidence**:
  - Trigger path: `index` command runs `runIndex` and calls `s.WriteIndex(...)`.
  - Reachability proof (snipe):
    - `snipe search "RunE: runIndex"` → `cmd/index.go:33`
    - `snipe search "s.WriteIndex(symbols, refs, edges)"` → `cmd/index.go:182`
  - Runtime failure (observed):
    - `snipe index --embed-mode off` failed with: `write index: truncate symbols: constraint failed: FOREIGN KEY constraint failed (787)`.
- **Mechanism**:
  - Full-reindex truncate path can hit FK violations when deleting `symbols`, causing complete index update failure.
- **Concrete failure scenario**:
  - User runs `snipe index` in CI or locally; command exits non-zero and leaves stale index, breaking downstream commands.
- **Minimal fix + tests**:
  - Fix: harden truncation by disabling FK checks for the bounded truncate transaction (or exhaustively delete every FK-dependent table and verify emptiness before deleting `symbols`).
  - Test: integration test that seeds all dependent tables, runs `WriteIndex` twice, and asserts second run succeeds.
- **Confidence**: High (reproduced in this environment).

#### 2) Incremental indexing silently ignores non-table-missing delete errors, risking stale/partial data
- **Severity**: High
- **Evidence**:
  - `WriteIndexIncremental` ignores errors unconditionally for multiple delete paths:
    - embeddings delete: `_ = err`
    - symbol_purposes delete: `_ = err`
    - imports delete: `_ = err`
  - Reachability proof (snipe):
    - `snipe search "incResult, err := s.WriteIndexIncremental"` → `cmd/index.go:580`
- **Mechanism**:
  - Real DB errors (I/O, lock, corruption) are suppressed and process continues to commit other mutations.
- **Concrete failure scenario**:
  - A transient DB error prevents deleting old imports; incremental write still commits new rows, leading to duplicate/stale import edges and inconsistent query output.
- **Minimal fix + tests**:
  - Fix: only suppress `no such table` errors; return all other errors.
  - Test: inject failing `DELETE` (or mock driver) and assert operation aborts with explicit error.
- **Confidence**: High (direct code inspection).

#### 3) Embedding response with negative index can panic process
- **Severity**: Medium
- **Evidence**:
  - In `Embed`, code checks `if d.Index < len(result)` then writes `result[d.Index] = ...` with no `d.Index >= 0` guard.
  - Reachability proof (snipe):
    - `snipe search "queryEmbed, err := client.EmbedOne"` → `cmd/sim.go:100`
    - `snipe search "embeddings, err := client.Embed(texts, \"document\")"` → `cmd/index.go:318`
- **Mechanism**:
  - Malformed/unexpected upstream payload containing `index: -1` satisfies `< len(result)` and causes panic on negative slice index.
- **Concrete failure scenario**:
  - During `snipe sim` or `snipe index --embed-mode realtime`, provider anomaly or proxy corruption returns invalid index; CLI crashes.
- **Minimal fix + tests**:
  - Fix: require `0 <= d.Index && d.Index < len(result)`.
  - Test: mock HTTP response with negative index and assert error (no panic).
- **Confidence**: High.

---

## PASS 2 — Concurrency & Lifecycle Audit

### Concurrency Roots Inventory
1. **Signal goroutine** in `PersistentPreRunE` (`cmd/root.go`) for SIGINT/SIGTERM cancellation.
2. **Watch event loop** in `runWatch` (`cmd/watch.go`) driven by fsnotify channels and debounce timer.
3. **Multipart streaming goroutine** in `BatchClient.UploadFile` (`internal/embed/batch.go`) writing into `io.Pipe`.

### Findings

#### 1) Watch mode misses changes in newly created directories (lifecycle asymmetry)
- **Severity**: Medium
- **Evidence + lifecycle trace**:
  - Start: `runWatch` calls recursive `addWatchDirs` once at startup.
  - Runtime: event loop filters `.go` file events, but never adds watcher on newly created dirs.
  - Reachability proof (snipe): `snipe search "RunE: runWatch"` → `cmd/watch.go:55`.
- **Mechanism**:
  - fsnotify does not recurse automatically; without adding watches for new directories, later file writes under those dirs are invisible.
- **Timeline failure scenario**:
  - Start `snipe watch` → create `pkg/newmod/` → add `pkg/newmod/new.go` → no reindex triggered.
- **Minimal fix**:
  - On create/rename directory events, detect `os.Stat(event.Name).IsDir()` and call `watcher.Add`.
- **Test strategy**:
  - Integration test creating a new directory after watch start and asserting a reindex event for added `.go` file.
- **Confidence**: High.

#### 2) Watch reindex subprocess is not context-bound; shutdown can block until full index completes
- **Severity**: Medium
- **Evidence + lifecycle trace**:
  - `runWatch` handles `GetContext().Done()` in select loop.
  - Reindex path calls blocking `runReindex(dir)` using `exec.Command(...)` (not `exec.CommandContext`).
  - While blocked in `cmd.Run()`, watch loop cannot process cancel event.
- **Mechanism**:
  - Cancellation is cooperative in loop but not propagated to child process lifecycle.
- **Timeline failure scenario**:
  - Debounce fires → long `snipe index` starts → operator presses Ctrl+C → parent remains blocked until child exits.
- **Minimal fix**:
  - Use `exec.CommandContext(GetContext(), exe, "index")` and optionally kill process group on cancel.
- **Test strategy**:
  - Start watch with short debounce, trigger reindex, cancel context, assert subprocess terminates promptly.
- **Confidence**: High.

#### 3) Batch upload can leak goroutine on early request-construction failure (Plausible)
- **Severity**: Low
- **Evidence + lifecycle trace**:
  - `UploadFile` starts writer goroutine before `http.NewRequest(...)`.
  - If request construction fails, function returns while writer goroutine may block on pipe write.
- **Mechanism**:
  - Pipe writer has no reader if request creation fails after goroutine launch.
- **Timeline failure scenario**:
  - Misconfigured `baseURL` or malformed URL in tests causes `NewRequest` error and leaked goroutine/file handle.
- **Minimal fix**:
  - Construct/validate request URL before starting goroutine, or close pipe on request-construction error.
- **Test strategy**:
  - Unit test with invalid URL and goroutine leak detector.
- **Confidence**: Medium (Plausible; depends on configuration mutation).

---

## PASS 3 — Persistence & Boundary Audit

### Boundary Inventory
- **DB write boundaries**: `Store.WriteIndex`, `Store.WriteIndexIncremental`, `WriteImports`, `WriteFiles`.
- **DB read boundaries**: query package under `internal/query/*`, plus context builders.
- **HTTP boundaries**: Voyage realtime embeddings (`internal/embed/client.go`) and batch APIs (`internal/embed/batch.go`).
- **File boundaries**: lock file + JSONL batch state and payload files.

### Findings

#### 1) Incremental write path can commit partial state due suppressed boundary errors
- **Severity**: High
- **Evidence + boundary trace**:
  - entrypoint: `index` command incremental path (`cmd/index.go`) → boundary: `Store.WriteIndexIncremental`.
  - Within boundary, deletion errors for optional tables are dropped unconditionally.
- **Mechanism**:
  - Non-idempotent mutation sequence proceeds despite failed cleanup step; resulting state reflects only subset of intended write-set.
- **Concrete failure scenario**:
  - `imports` delete fails under lock contention; new imports inserted anyway; query surfaces stale and duplicate import relationships.
- **Minimal fix**:
  - Treat unexpected delete errors as fatal and rollback transaction.
- **Test plan**:
  - Fault-injection test where `DELETE FROM imports` fails; assert transaction rollback and unchanged DB.
- **Confidence**: High.

#### 2) Full reindex boundary is brittle to FK-dependent residue and can fail atomically with no recovery path
- **Severity**: High
- **Evidence + boundary trace**:
  - entrypoint: `runIndex` full path → `Store.WriteIndex` truncate+insert transaction.
  - Observed boundary failure at truncate stage (FK constraint) during real run.
- **Mechanism**:
  - All-or-nothing transaction aborts before rewrite; operationally causes persistent indexing failures until manual intervention.
- **Concrete failure scenario**:
  - CI pipeline that refreshes index per run starts failing after schema/data drift and remains failed for all users.
- **Minimal fix**:
  - Add explicit FK-consistency preflight (`PRAGMA foreign_key_check`) and recovery strategy (force rebuild with FK temporarily disabled in controlled transaction).
- **Test plan**:
  - Regression test with seeded FK-residue reproducer; assert rebuilt index succeeds and counts are sane.
- **Confidence**: High.

#### 3) Embedding HTTP boundary does not propagate command context cancellation
- **Severity**: Medium
- **Evidence + boundary trace**:
  - Request creation uses `http.NewRequest` without context in both realtime and batch clients.
  - CLI has cancellable context (`GetContext()`), but it is not threaded into HTTP boundaries.
- **Mechanism**:
  - User cancellation/timeout may not interrupt in-flight network calls promptly; increases hang/slow shutdown risk.
- **Concrete failure scenario**:
  - Network stall during `snipe sim` or embedding index; command timeout occurs but HTTP waits until client timeout (30–120s).
- **Minimal fix**:
  - Accept context in embedding APIs and use `http.NewRequestWithContext`.
- **Test plan**:
  - Test server that blocks response; cancel context and assert immediate return.
- **Confidence**: Medium-High.
