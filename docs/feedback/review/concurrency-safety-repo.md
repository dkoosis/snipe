---
review: concurrency-safety
scope: repo
review_date: 2026-05-17
race_detector_run: true
race_detector_result: clean
packages_analyzed: cmd, internal/store, internal/util, internal/embed, internal/kg, internal/index
findings: 3
critical: 0
high: 0
medium: 2
info: 1
run_id: ecebe5258308
---

# concurrency-safety — snipe

Tier: 🟢 (overall). Race detector passed across all 20 packages (`go test -race -timeout=180s ./...`, all `ok`). Concurrency surface is small: 3 production goroutines (signal handler in `cmd/root.go`, reindex launcher in `cmd/watch.go`, multipart pipe writer in `internal/embed/batch.go`), 1 RWMutex (`internal/util.FileCache`), 2 channels, 3 context derivations. No proven race, deadlock, or leak. Findings below are latency/cancellation hygiene, not correctness defects.

## Findings

### F1 — Incremental store writes ignore caller context

- **File:** `internal/store/write.go:441`
- **Function:** `Store.WriteIndexIncremental`
- **Severity:** Medium
- **Category:** context
- **Rule:** context-not-propagated
- **Sequence:** `watch` launches `runReindex` → subprocess `snipe index` calls `WriteIndexIncremental` → user Ctrl-C cancels `cmdCtx` → subprocess receives SIGTERM via `exec.CommandContext`, but the in-flight SQLite transaction is mid-statement on `context.Background()`-bound calls and cannot be interrupted by `ctx`. The subprocess is killed by signal, but library callers (`metrics/capture.go:152`, future in-process callers) get no cancellation path.
- **Code:**
  ```go
  func (s *Store) WriteIndexIncremental(...) (res *IncrementalResult, err error) {
      ...
      conn, err := s.db.Conn(context.Background())              // ignores caller ctx
      _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys=OFF")
      tx, err := conn.BeginTx(context.Background(), nil)
  ```
  Sibling `WriteIndex` (line 34) has the same shape — no ctx parameter at all.
- **Fix:** Take `ctx context.Context` as the first parameter; thread it through `db.Conn`, `ExecContext`, `BeginTx`. Update the two callers (`cmd/index.go:693,744`) to pass `cmd.Context()`.
- **Repro:** `snipe watch` with a slow disk, edit a file to trigger reindex, Ctrl-C during the SQL transaction — subprocess exits only when SIGTERM lands, not when ctx cancels.

### F2 — `context.Background` in package-loader fallback hides cancellation gap

- **File:** `internal/index/loader.go:63`
- **Function:** `Load`
- **Severity:** Medium
- **Category:** context
- **Rule:** context-not-propagated
- **Sequence:** `Load(cfg)` falls back to `context.Background()` when `cfg.Context == nil`. `go/packages.Config.Context` then drives the `go list` / `go build` subprocess. A caller that forgets to set `cfg.Context` silently loses the ability to cancel a multi-minute package load.
- **Code:**
  ```go
  func Load(cfg LoadConfig) (*LoadResult, error) {
      ctx := cfg.Context
      if ctx == nil {
          ctx = context.Background()   // silent fallback
      }
  ```
- **Fix:** Either (a) make `Context` required and return an error if nil, or (b) accept ctx as a first parameter (`Load(ctx, cfg)`) and remove `LoadConfig.Context`. Option (b) matches Go convention and removes the footgun.
- **Repro:** Audit callers; any path that constructs a `LoadConfig` literal without setting `Context` is uncancellable.

### F3 — `FileCache.LoadLines` cache-hit path takes write lock for accessTime bookkeeping

- **File:** `internal/util/file.go:79`
- **Function:** `FileCache.LoadLines`
- **Severity:** Info
- **Category:** contention
- **Rule:** rwmutex-over-locked
- **Sequence:** Every cache hit takes `mu.Lock()` (exclusive) to bump `cached.accessTime`. Under concurrent read load all hits serialize even though the cache map itself is unchanged. The `globalFileCache` in `internal/output/json.go:17` is package-global; if `snipe` ever runs queries concurrently (server mode, parallel orca calls), this becomes the bottleneck.
- **Code:**
  ```go
  func (fc *FileCache) LoadLines(path string) ([]string, error) {
      fc.mu.Lock()                                  // exclusive even on hit
      if cached, ok := fc.cache[path]; ok {
          cached.accessTime = time.Now()
          lines := cached.lines
          fc.mu.Unlock()
          return lines, nil
      }
      fc.mu.Unlock()
      ...
  ```
- **Fix:** Either (a) make `accessTime` an `atomic.Int64` (UnixNano) and use `RLock()` on the hit path, or (b) accept eventual-consistency LRU and drop the per-hit update entirely (update only on miss/eviction). Option (a) preserves LRU accuracy without serializing readers.
- **Repro:** Current single-threaded CLI use — no observable impact. Flag for any future concurrent caller.

## Pre-work output

- `go test -race -timeout=180s ./...` — all 20 packages `ok`, no race-detector hits.
- Goroutine launches in non-test, non-vendor code: 3 (`cmd/root.go:96` signal handler, `cmd/watch.go:105` reindex, `internal/embed/batch.go:172` multipart pipe writer). All have clean termination paths (ctx.Done / buffered chan / `defer close(done)` + `<-done`).
- `sync` primitives in non-test, non-vendor code: 1 (`internal/util.FileCache.mu sync.RWMutex`).
- `make(chan ...)` in non-test, non-vendor code: 2 (`cmd/root.go:94` buffered 1 for signal, `cmd/watch.go:95` buffered 1 for reindex result, `internal/embed/batch.go:171` unbuffered `done`). All bounded, all read.
- `errgroup` / `singleflight` / `semaphore`: 0 in project code.

## Notes / non-findings considered and rejected

- `cmd/root.go:96` signal goroutine: terminates on either `<-sigCh` or `<-ctx.Done()`. PostRun calls `cmdCancel`. Not a leak.
- `cmd/watch.go:105` reindex goroutine: `indexing` flag prevents overlap, `reindexDone` is buffered (size 1) so a send after parent return is absorbed; subprocess killed by `exec.CommandContext`. Clean.
- `internal/embed/batch.go:172` pipe-writer goroutine: paired `defer pr.Close(); <-done` guarantees exit even if `http.NewRequest` fails before `client.Do`. Comment at line 178 documents this. Clean.
- `cmdCtx` / `cmdCancel` as package globals (`cmd/root.go:52`): single CLI invocation per process, no concurrency. Acceptable.
- `cmd/embed.go:216` `<-time.After`: paired with `<-cmd.Context().Done()` in a select. Not a sleep-as-sync.
