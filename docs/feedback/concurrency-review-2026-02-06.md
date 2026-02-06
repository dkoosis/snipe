---
review_type: concurrency
review_date: 2026-02-06
reviewer: Claude (go-concurrency-reviewer)
codebase_root: .
focus_files: ["all (excluding vendor for source analysis)"]
race_detector_run: true
race_detector_log: docs/feedback/race-detector.log
total_findings: 5
summary:
  critical: 0
  high: 1
  medium: 2
  info: 2
---

# Go Concurrency Review - 2026-02-06

## Executive Summary

| Severity | Count | Icon |
|----------|-------|------|
| 🔴 Critical - Deadlock/Race | 0 | 💀 |
| 🟠 High - Goroutine Leak | 1 | 💧 |
| 🟡 Medium - Contention | 2 | 🐢 |
| 🔵 Info - Non-Idiomatic | 2 | 🎨 |

Race detector completed for repository packages and did not report any runtime data races in covered tests. However, static review found one high-confidence unsynchronized write in production code that is not currently exercised by tests under `-race`.

**Hotspot Packages:** (Top by goroutine-launch keyword count, excluding vendor)
- `magefile.go` - 7 matches (shell command strings)
- `internal/store/write_test.go` - 4 matches
- `internal/context/generate.go` - 4 matches (documentation strings)
- `internal/index/fingerprint.go` - 3 matches (comments/strings)
- `internal/store/store_test.go` - 2 actual goroutine launches

## Top Priority Fixes (Critical/High)

### 1. Unsynchronized write under `RLock` in file cache metadata update

**File:** `internal/util/file.go:40`
**Function:** `(*FileCache).LoadLines`
**Severity:** High
**Category:** race
**Principle Violated:** CorrectLocking

**Finding:**
`LoadLines` acquires `RLock`, then mutates shared state (`cached.accessTime = time.Now()`) before releasing the read lock.

**Analysis:**
A read lock allows concurrent readers. Mutating `accessTime` while only holding `RLock` can race with other readers/writers touching the same cache entry. In production this can manifest as nondeterministic cache eviction behavior and potential data race panics when validated with sufficient race-test coverage.

**Code Snippet:**
```go
fc.mu.RLock()
if cached, ok := fc.cache[path]; ok {
    cached.accessTime = time.Now() // write under RLock
    lines := cached.lines
    fc.mu.RUnlock()
    return lines, nil
}
fc.mu.RUnlock()
```

**Race Detector Output:**
No direct hit in current test coverage (`docs/feedback/race-detector.log`), indicating missing execution path coverage rather than safety.

**Recommendation:**
Use one of:
1. Promote to `Lock()` for the metadata update path.
2. Store access timestamp with an atomic type decoupled from map lock.
3. Avoid mutating on reads and update recency only in write paths.

Minimal safe option:
```go
fc.mu.Lock()
if cached, ok := fc.cache[path]; ok {
    cached.accessTime = time.Now()
    lines := cached.lines
    fc.mu.Unlock()
    return lines, nil
}
fc.mu.Unlock()
```

**Repro Command:**
```bash
go test -race ./internal/util -run TestFileCache -count=100
```

**Acceptance Criteria:**
- [ ] New/updated test exercises concurrent cache hits and passes with `-race`
- [ ] No write occurs while holding `RLock`
- [ ] Cache hit latency remains acceptable under benchmark

**Labels:** `P1`, `concurrency`, `race`

---

## Additional Findings

### Medium

#### 2. Long-running watch loop does not honor command context cancellation

**File:** `cmd/watch.go:89`
**Function:** `runWatch`
**Severity:** Medium
**Category:** context
**Principle Violated:** ContextAwareness

**Finding:**
`runWatch` loops forever on watcher channels and timer without selecting on `GetContext().Done()`.

**Analysis:**
This makes graceful shutdown dependent on process signal behavior rather than explicit cancellation propagation used elsewhere in CLI lifecycle. It also complicates embedding/testing where cancellation is expected through context.

**Recommendation:**
Add a context-aware select branch:
```go
ctx := GetContext()
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    // existing cases...
    }
}
```

**Repro Command:**
```bash
go test ./cmd -run Watch
```

**Labels:** `P2`, `concurrency`, `context`

#### 3. Child reindex process not bound to cancellation context

**File:** `cmd/watch.go:216`
**Function:** `runReindex`
**Severity:** Medium
**Category:** context
**Principle Violated:** ContextAwareness

**Finding:**
`exec.Command` is used instead of `exec.CommandContext`, so cancellation may not terminate in-flight reindex subprocesses.

**Analysis:**
Under frequent file changes, stale reindex processes can outlive caller intent and consume CPU/IO until they exit naturally.

**Recommendation:**
Use `exec.CommandContext(GetContext(), exe, "index")` and propagate timeout/cancel semantics.

**Repro Command:**
```bash
snipe watch --debounce 1
```

**Labels:** `P2`, `concurrency`, `context`

### Informational

#### 4. Signal channel goroutine in PreRunE created before all failure points

**File:** `cmd/root.go:82`
**Function:** `rootCmd.PersistentPreRunE`
**Severity:** Info
**Category:** leak
**Principle Violated:** GoroutineLifecycle

**Finding:**
Signal-listener goroutine is started before config loading. If `config.Load` fails, `PersistentPostRun` may not run, so `cmdCancel()` is not guaranteed.

**Analysis:**
In current CLI process model this is low impact (process typically exits immediately), but it is a lifecycle asymmetry.

**Recommendation:**
Start signal goroutine only after successful configuration load, or `defer cmdCancel()` on PreRunE error paths.

**Repro Command:**
```bash
snipe --config /nonexistent status
```

**Labels:** `P3`, `concurrency`, `lifecycle`

#### 5. Timer reset pattern may trigger extra wakeups under bursty events

**File:** `cmd/watch.go:126`
**Function:** `runWatch`
**Severity:** Info
**Category:** contention
**Principle Violated:** ChannelSafety

**Finding:**
Debounce timer is reset directly without explicit stop/drain sequence.

**Analysis:**
Current single-goroutine select loop avoids hard races, but stale timer events can still cause immediate wakeups and unnecessary loop iterations during heavy event bursts.

**Recommendation:**
Use canonical stop-drain-reset helper before `Reset` when coalescing bursts.

**Repro Command:**
```bash
go test ./cmd -run Watch -count=20
```

**Labels:** `P3`, `concurrency`, `debounce`

## Analysis Configuration

- **Review Date:** 2026-02-06
- **Code Root:** `/workspace/snipe`
- **Focus Files:** all repo Go files (excluding `vendor/` for static review signal quality)
- **Race Detector:** executed; log at `docs/feedback/race-detector.log`
- **Tools Used:** ripgrep, go test -race, manual code inspection
- **Packages Analyzed:** 16 (`go list ./...`)
- **Hotspot Packages:** derived from `rg 'go (func|[a-z])' --count-matches --type go -g '!vendor/**'`
