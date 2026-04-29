# True-Bug Audit (2026-04-29)

## PASS 1 — Correctness & Reliability

### System Map
- Entry points: Cobra CLI commands rooted at `cmd/root.go` (`Execute` -> `rootCmd.Execute` -> subcommand `RunE`).
- Concurrency: command-level signal handler goroutine (`cmd/root.go`), batch upload streaming goroutine (`internal/embed/batch.go`).
- Persistence: SQLite transactions in `internal/store/*`.
- External boundaries: Voyage AI HTTP requests via `internal/embed/client.go` and `internal/embed/batch.go`.
- Error handling: mostly wrapped with `%w`; command-level JSON envelope errors via `internal/output`.

### Findings

1. **Missing context propagation to outbound HTTP requests (batch embedding path)**  
   Severity: **High**  
   Evidence: `UploadFile`, `CreateBatch`, and `GetBatchStatus` create requests with `http.NewRequest(...)` and never bind command cancellation context. Reachability: `snipe callers UploadFile`, `snipe callers CreateBatch`, `snipe callers GetBatchStatus` show CLI embedding flows invoke these paths.  
   Mechanism: process receives cancellation (`cmdCancel`) but in-flight HTTP requests continue until client timeout, delaying shutdown and causing operational hangs.  
   Failure scenario: user runs embedding command with `--timeout=5s`; request is still running up to 60s because request context is detached.  
   Minimal fix: pass `context.Context` into batch client methods and use `http.NewRequestWithContext`. Add cancellation test with `httptest.Server` blocking handler.  
   Confidence: **High**.

2. **Potential file descriptor leak when early-returning in row-iteration helper paths**  
   Severity: **Medium (Plausible)**  
   Evidence: mixed row-close patterns (`defer rows.Close()` and manual `_ = rows.Close()`) across context generation flows (`internal/context/generate.go`). Some nested-query paths avoid defer by design and rely on explicit close branches.  
   Mechanism: future branch additions can bypass explicit close and leak descriptors under repeated runs.  
   Failure scenario: repeated context generation in long-running automation eventually hits sqlite "too many open files".  
   Minimal fix: centralize row lifecycle in helper utilities and require `defer` unless proven unsafe; add stress test over repeated generation.  
   Confidence: **Medium**.

## PASS 2 — Concurrency & Lifecycle

### Concurrency Roots Inventory
- `cmd/root.go`: goroutine handling OS signals and cancelling command context.
- `internal/embed/batch.go`: goroutine writing multipart content to `io.Pipe`.
- Test-only goroutines in `internal/store/store_test.go`.

### Findings

1. **Multipart writer goroutine can outlive caller on request construction/send failures**  
   Severity: **Medium**  
   Evidence: `UploadFile` launches goroutine before `http.NewRequest` / `client.Do`; early return paths do not explicitly close pipe reader/writer from caller side. Reachability: `snipe callers UploadFile` -> embedding command flow.  
   Mechanism: producer goroutine may block writing to pipe when consumer path aborts early.  
   Timeline scenario: transient allocation/network error triggers early return; goroutine remains blocked until GC/fd teardown. Repeated failures accumulate goroutines.  
   Minimal fix: create request first where possible; or `defer pr.Close()` / caller-driven cancel context; gate goroutine on ctx.Done.  
   Test strategy: inject failing transport and assert goroutine count stabilizes after retries.  
   Confidence: **Medium**.

2. **Signal handler lifecycle is per-command but global state is package-level**  
   Severity: **Low (Plausible)**  
   Evidence: globals `cmdCtx`, `cmdCancel` in `cmd/root.go`; handler goroutine reads shared cancel func.  
   Mechanism: if command execution model evolves to parallel subcommand execution (library embedding), shared mutable globals can race/cancel wrong command.  
   Timeline scenario: host application runs two commands concurrently; interrupt cancels latest command only or races.  
   Minimal fix: store context/cancel in command-scoped struct, avoid package globals.  
   Test strategy: race test invoking command execution concurrently in-process.  
   Confidence: **Low-Medium**.

## PASS 3 — Persistence & Boundary

### Boundary Inventory
- DB writes: transactional methods in `internal/store/write.go`, `internal/store/metrics.go`, `internal/store/sccs.go`.
- DB reads: `internal/query/*`, `internal/context/*`.
- HTTP boundaries: `internal/embed/client.go`, `internal/embed/batch.go`.
- Filesystem writes: batch JSONL/output file creation in `internal/embed/batch.go`.

### Findings

1. **Boundary cancellation gap on HTTP requests (duplicates Pass 1 due to severity)**  
   Severity: **High**  
   Evidence: request creation without context in batch client methods; command-level context exists but is not threaded into boundary calls.  
   Mechanism: boundary ignores caller cancellation and can continue side effects beyond caller timeout.  
   Scenario: cancelled command still uploads file/create batch remotely, causing unexpected costs and duplicate later retries.  
   Minimal fix: `context.Context` plumb-through + idempotency guard using persisted batch state before retries.  
   Test plan: integration test with cancelled context verifies no outbound call completion after cancel.  
   Confidence: **High**.

2. **Transaction boundary correctness risk if rollback errors are ignored in some paths**  
   Severity: **Low (Plausible)**  
   Evidence: some transaction defers drop rollback errors (`internal/store/metrics.go`, `internal/store/sccs.go`) while others wrap/log rollback failures.  
   Mechanism: rollback I/O errors become invisible, complicating corruption diagnosis on disk/full or fsync anomalies.  
   Scenario: commit fails, rollback also fails; operator sees only top-level error and misses data-at-risk signal.  
   Minimal fix: standardize rollback handling to surface rollback error context (joined error).  
   Test plan: mock driver or fault-injection test forcing rollback failure path.  
   Confidence: **Medium**.

## Reachability commands executed
- `snipe doctor`
- `snipe callers UploadFile`
- `snipe callers CreateBatch`
- `snipe callers GetBatchStatus`
- `snipe pack UploadFile`
