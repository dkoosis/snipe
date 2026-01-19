---
review_type: concurrency
review_date: 2026-01-19
reviewer: Claude (go-concurrency-reviewer)
codebase_root: .
focus_files: ["all"]
race_detector_run: true
race_detector_log: docs/feedback/race-detector.log
total_findings: 3
summary:
  critical: 0
  high: 1
  medium: 1
  info: 1
---

# Go Concurrency Review - 2026-01-19

## Executive Summary

| Severity | Count | Icon |
|----------|-------|------|
| 🔴 Critical - Deadlock/Race | 0 | 💀 |
| 🟠 High - Goroutine Leak | 1 | 💧 |
| 🟡 Medium - Contention | 1 | 🐢 |
| 🔵 Info - Non-Idiomatic | 1 | 🎨 |

**Hotspot Packages:** (Top by goroutine count)
- `internal/store` - 2 goroutine launches
- `cmd` - 1 goroutine launch

## Top 5 Priority Fixes

### 1. FileCache updates access time under read lock (data race)

**File:** `internal/util/file.go:36`
**Function:** `LoadLines`
**Severity:** High
**Category:** race
**Principle Violated:** CorrectLocking

**Finding:**
`LoadLines` updates `cached.accessTime` while holding an `RLock`, which is a write to shared state under a read lock. Concurrent readers can mutate the same field without mutual exclusion, creating a data race and violating RWMutex semantics.

**Analysis:**
Any concurrent call to `LoadLines` can write `accessTime` under an `RLock`. Meanwhile, eviction logic in `evictOldest` relies on consistent timestamps to decide which entry to delete. This can lead to race detector failures, undefined behavior, and inconsistent eviction in production. The race detector did not surface this in current tests, but the pattern is inherently unsafe.

**Code Snippet:**
```go
// internal/util/file.go:34-41
fc.mu.RLock()
if cached, ok := fc.cache[path]; ok {
    cached.accessTime = time.Now()
    lines := cached.lines
    fc.mu.RUnlock()
    return lines, nil
}
```

**Race Detector Output:**
None observed in `docs/feedback/race-detector.log`.

**Recommendation:**
Promote to a write lock for the access-time update or use a safe two-step pattern:
```go
fc.mu.RLock()
cached, ok := fc.cache[path]
lines := cached.lines
fc.mu.RUnlock()
if ok {
    fc.mu.Lock()
    if cached, ok := fc.cache[path]; ok {
        cached.accessTime = time.Now()
    }
    fc.mu.Unlock()
    return lines, nil
}
```
This keeps the fast path mostly read-locked while making the write safe.

**Repro Command:**
```bash
go test -race ./internal/util
```

**Acceptance Criteria:**
- [ ] `go test -race ./internal/util` passes without data race warnings.
- [ ] Cache eviction still behaves deterministically under concurrent access.

**Labels:** `P1`, `concurrency`, `race`

---

## Additional Findings

### Medium

#### Watch command ignores cancellation context

**File:** `cmd/watch.go:73`
**Function:** `runWatch`
**Severity:** Medium
**Category:** context
**Principle Violated:** ContextAwareness

**Finding:**
The long-running event loop in `runWatch` never checks the command context for cancellation. `PersistentPreRunE` installs a signal handler that cancels the context on Ctrl+C, but `runWatch` does not observe it, so SIGINT may not stop the watcher.

**Analysis:**
This is a shutdown path issue: the goroutine tied to signal handling cancels the context, but the watch loop ignores it. In practice, users may see the CLI continue running after Ctrl+C, leading to stuck processes and forced kills.

**Code Snippet:**
```go
// cmd/watch.go:69-83
for {
    select {
    case event, ok := <-watcher.Events:
        if !ok {
            return nil
        }
    // ...
    }
}
```

**Recommendation:**
Add a `case <-cmd.Context().Done(): return cmd.Context().Err()` branch (or use `GetContext()` to avoid dependency on cobra context), and ensure the watcher is closed on exit.

**Repro Command:**
```bash
snipe watch
# Press Ctrl+C and observe process exit behavior.
```

**Acceptance Criteria:**
- [ ] Ctrl+C exits `snipe watch` cleanly within 1s.
- [ ] The watcher closes without leaking goroutines.

**Labels:** `P2`, `concurrency`, `context`

### Informational

#### Query helpers lack context-aware DB calls

**File:** `internal/query/lookup.go:10`
**Function:** `LookupByID` (and peers)
**Severity:** Info
**Category:** context
**Principle Violated:** ContextAwareness

**Finding:**
Query helper functions use `db.Query`/`db.QueryRow` without `context.Context`, which prevents cancellation of long-running database queries and complicates graceful shutdown.

**Analysis:**
While not currently a bug, this becomes important in long-running CLI commands or future server modes. Context-aware DB APIs (`QueryContext`, `QueryRowContext`) would allow the root command’s cancellation path to stop in-flight queries.

**Code Snippet:**
```go
// internal/query/lookup.go:31-38
err := db.QueryRow(`
    SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
           s.signature, s.doc, s.receiver, f.hash
    FROM symbols s
    LEFT JOIN files f ON s.file_path = f.path
    WHERE s.id = ?
`, id).Scan(&s.ID, &s.Name, &s.Kind, &s.FilePath, &filePathRel, &pkgPath, &s.LineStart, &s.ColStart, &s.LineEnd, &s.ColEnd,
    &s.Signature, &s.Doc, &s.Receiver, &fileHash)
```

**Recommendation:**
Introduce context-aware query APIs and thread `context.Context` from the caller, following the TODO noted in this file.

**Repro Command:**
```bash
go test ./internal/query
```

**Acceptance Criteria:**
- [ ] Query helpers accept `context.Context` as the first argument.
- [ ] Callers pass the root command context into DB queries.

**Labels:** `P3`, `concurrency`, `context`

## Analysis Configuration

- **Review Date:** 2026-01-19
- **Code Root:** .
- **Focus Files:** all
- **Race Detector:** true (log at `docs/feedback/race-detector.log`)
- **Tools Used:** ripgrep, go test -race
- **Packages Analyzed:** 18
- **Hotspot Packages:** `internal/store` (2 goroutine launches), `cmd` (1 goroutine launch)
