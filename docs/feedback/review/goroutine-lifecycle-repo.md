# goroutine-lifecycle — snipe (repo scope)

Run: `ecebe5258308` · scope: project · Go directive: `go 1.24.0` (loop-var rescope ✓)
Launches inspected (non-vendor): 5 production-adjacent + test sites. Vendor sites excluded.

## Summary

| Tier | P1 ownership | P1 ctx | P1 shared | P2 magic | P2 lock | P3 idiom |
|---|---|---|---|---|---|---|
| Status | 🟡 | 🟡 | 🟢 | 🟡 | 🟢 | 🟢 |

`cmd/root.go` signal handler and `internal/embed/batch.go` pipe producer are well-owned. The watch reindex goroutine is the main design weakness — fire-and-forget with a send that can block if the loop exits early. Test sleeps in two files are borderline (one bounds a cancel, one bounds a leak assertion).

## Findings

### 1. [F1] `cmd/watch.go:105` — goroutine-no-owner

**Diagnosis.** `startReindex` launches an unbounded indexer goroutine with no Wait/Stop handle; its only exit signal is a send to `reindexDone` (buffer=1).
**Why.** If the main `for/select` loop returns first (watcher.Events close at line 116, watcher.Errors close at 214, or a future early-return), the still-running goroutine eventually completes `runReindex` and blocks forever on `reindexDone <- …` — there's no receiver and no `select { case ... case <-ctx.Done(): }` on the send. `runReindex` itself uses `exec.CommandContext(GetContext(), …)` so cancellation will kill the child, but the parent goroutine still tries to deliver the result.
**Evidence.** Launch at `cmd/watch.go:105-109`; receiver at `cmd/watch.go:182`; only exit channels from the loop are at lines 116, 214, neither drains `reindexDone`.
**Fix.** Either (a) add `case <-ctx.Done(): return` around the send, or (b) before the loop exits, drain `reindexDone` if `indexing`. Option (a) is simpler.

Tier: action

### 2. [F2] `cmd/watch.go:105` — goroutine-ignores-ctx

**Diagnosis.** The reindex goroutine doesn't select on `GetContext().Done()`; it depends on `exec.CommandContext` propagating cancellation to the child process.
**Why.** Works today because the only blocking call is `cmd.Run()`. But this is implicit ownership — any future addition (pre-check, post-process, retry loop) inside the goroutine body inherits the no-ctx-check pattern and turns silent leaks into real ones. Related to F1: same goroutine, separate smell.
**Evidence.** `cmd/watch.go:105-109` — body is `start := …; err := runReindex(dir); reindexDone <- {…}`. No `select` envelope.
**Fix.** Wrap the send in `select { case reindexDone <- res: case <-GetContext().Done(): }` — fixes F1 and F2 together.

Tier: action

### 3. [F3] `cmd/root.go:96` — goroutine-no-owner (info — process-lifetime)

**Diagnosis.** PersistentPreRunE launches a signal-watcher goroutine; ownership is the implicit `ctx.Done()` arm of its own select.
**Why.** Per linter rule: "don't flag long-lived process-lifetime goroutines in `main` — note as Info." Goroutine exits cleanly on either `sigCh` or `ctx.Done()`, calls `signal.Stop(sigCh)`. Design is correct.
**Evidence.** `cmd/root.go:94-104`. Both arms terminate; `cmdCancel` is captured before the goroutine launches.
**Fix.** None. Recorded so the audit trail is complete.

Tier: borderline

### 4. [F4] `internal/embed/batch.go:172` — goroutine-no-owner (clean)

**Diagnosis.** Pipe-producer goroutine is paired with `done := make(chan struct{})` and `defer { _ = pr.Close(); <-done }`.
**Why.** Spot-check: launch + `pw.CloseWithError(err)` + deferred `pr.Close()` form the documented "always-unblock" pattern. Test at `batch_test.go:285+` asserts no goroutine leak across 10 failed iterations. Design is correct.
**Evidence.** `internal/embed/batch.go:167-184` plus leak test `internal/embed/batch_test.go:285-315`.
**Fix.** None.

Tier: borderline

### 5. [F5] `internal/kg/hints_test.go:117` — time-sleep-as-sync (test)

**Diagnosis.** `go func() { time.Sleep(50ms); cancel() }()` schedules a delayed cancel to test ctx-honoring.
**Why.** The assertion is qualitative ("elapsed < 3s") with generous slack vs. the 6s worst case — exactly the borderline shape the rule calls out ("sleeps that gate a qualitative assertion with a generous upper bound"). Could be replaced by a `ready` channel synchronised on subprocess start, but that requires an instrumentation hook into `GetHints`. Cost > benefit for this test.
**Evidence.** `internal/kg/hints_test.go:116-120`; comment at line 129-130 documents the timing budget.
**Fix.** Report-only. If churned, add `//lintbrush:disable=goroutine-lifecycle:time-sleep-as-sync timing-budgeted ctx-cancel test`.

Tier: borderline

### 6. [F6] `internal/embed/batch_test.go:299,310` — time-sleep-as-sync (test)

**Diagnosis.** Two `time.Sleep`s bracket a goroutine-leak assertion (`runtime.NumGoroutine` before/after).
**Why.** These wait for the scheduler/finalizers to settle around `runtime.GC()` — there is no signal to wait on (runtime goroutines aren't observable). This is the canonical exception. Bounded (20ms, 50ms), used once per test, not in a prod path.
**Evidence.** `internal/embed/batch_test.go:297-300, 308-311`. Comments document intent ("Settle pre-existing goroutines", "Allow finalizers/scheduler to settle").
**Fix.** Report-only. Consider `//lintbrush:disable=…` marker if future passes re-flag.

Tier: borderline

### 7. [F7] `internal/store/store_test.go:222` — chan-instead-of-waitgroup (mixed)

**Diagnosis.** Test uses `done := make(chan struct{})` + `go func() { wg.Wait(); close(done) }()` to make `wg.Wait()` selectable against `errChan`.
**Why.** This is the legitimate "channel as cancellation/select-arm" exception — the linter rule explicitly excludes "channels used with select for timeout/cancellation." Not a bug. But the surrounding pattern has a latent issue worth recording: after `case err := <-errChan: t.Fatalf(...)`, the `close(errChan)` + range below is unreachable; if `<-done` arm fires first while goroutines are mid-send, the subsequent `close(errChan)` is still racy because `wg.Wait()` returned → no senders left, so actually safe. Borderline.
**Evidence.** `internal/store/store_test.go:189-237`.
**Fix.** Report-only — the channel-vs-WG choice is correct. Drive-by polish: the post-select `close(errChan)` + range never finds extra errors (Fatalf already exited); could be simplified but out of scope for this linter.

Tier: borderline

### 8. [F8] `cmd/watch.go:95` — chan-magic-buffer (justified)

**Diagnosis.** `reindexDone := make(chan reindexResult, 1)`.
**Why.** Buffer of 1 is intentional — it lets the reindex goroutine deliver its result without blocking while the main loop is processing a different `select` arm. This is the "single-shot signal" justification the rule allows. Not a magic 100/1024 buffer.
**Evidence.** `cmd/watch.go:95`. State machine guarantees at most one outstanding indexer (`indexing bool` gate at line 96, 173).
**Fix.** None. Mention here only to log the audit. Optional polish: one-line comment justifying the 1.

Tier: borderline

## Score

- P1 ownership: 🟡 (1 fire-and-forget with a real failure mode → F1)
- P1 ctx: 🟡 (1 gap, same site → F2)
- P1 shared state: 🟢 (no closures over pointers; Go 1.24 rescope handles loop vars; store_test uses param-passing `func(id int)`)
- P2 magic: 🟡 (2 test sleeps; both borderline-justified)
- P2 lock reentry: 🟢 (no critical sections call out to formatters/loggers in audited code)
- P3 idiom: 🟢 (chan-vs-WG choice in store_test is justified by the select arm)

Net: one real action item (F1+F2, single fix at `cmd/watch.go:105`). Everything else is report-only.
