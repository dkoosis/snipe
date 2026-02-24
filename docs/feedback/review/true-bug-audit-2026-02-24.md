# Go Codebase True-Bug Audit (3 Pass)

## PASS 1 — Correctness & Reliability

### System Map
- **Entrypoint:** CLI process starts in `main.main`, then `cmd.Execute`, then Cobra subcommands registered by `rootCmd.AddCommand(...)` in per-command `init()` functions.
- **Primary flows:**
  - `snipe index` (`runIndex`) builds symbols/refs/calls/imports and writes SQLite state.
  - Embedding flows branch into realtime (`generateEmbeddings`) or async batch (`startBatchEmbeddings` + `embed-status`).
  - `snipe watch` runs an infinite fsnotify loop and shells out to `snipe index`.
- **Persistence/boundaries:** SQLite (`internal/store`), Voyage HTTP API (`internal/embed/client.go`, `internal/embed/batch.go`), filesystem state under `.snipe/`.
- **Concurrency/lifecycle model:** mostly single-threaded command handlers with a few goroutines/signals in root pre-run, plus long-lived watch loop.

### Findings (ranked)

#### 1) Batch upload builds full multipart body in RAM (OOM risk)
- **Severity:** High
- **Evidence:** `UploadFile` copies entire JSONL into `bytes.Buffer` before request (`var buf bytes.Buffer`, `io.Copy(part, file)`, `http.NewRequest(..., &buf)`). Reachability: `runIndex` → `startBatchEmbeddings` → `(*BatchClient).UploadFile` (confirmed by `snipe callers startBatchEmbeddings` and `snipe callers "(*BatchClient).UploadFile"`).
- **Mechanism:** memory scales with full upload size (JSONL + multipart overhead), not streaming.
- **Failure scenario:** large monorepo generates large `.snipe/embeddings.jsonl`; process RSS spikes and gets OOM-killed mid-index, leaving partial/failed operational state.
- **Minimal fix + tests:** stream with `io.Pipe` + multipart writer goroutine and `http.NewRequest` body reader; add integration test with large synthetic JSONL asserting no giant in-memory buffer and successful upload via test server.
- **Confidence:** High

#### 2) Batch result download/parse is fully in-memory and duplicates large payloads
- **Severity:** High
- **Evidence:** `DownloadFile` returns `io.ReadAll(resp.Body)`; `downloadAndSaveEmbeddings` stores full `[]byte`; `ParseBatchResults` scans from `bytes.NewReader(data)` and accumulates all embeddings in `map[string][]float32` before DB writes. Reachability: `runEmbedStatus` → `downloadAndSaveEmbeddings` → `DownloadFile` + `ParseBatchResults` (confirmed by `snipe callers downloadAndSaveEmbeddings`, `snipe callers "(*BatchClient).DownloadFile"`, `snipe callers "(*BatchClient).ParseBatchResults"`).
- **Mechanism:** multiple whole-dataset resident structures create peak memory amplification.
- **Failure scenario:** completed batch with very large output causes embed-status to OOM during import; embeddings never persisted though remote batch succeeded.
- **Minimal fix + tests:** stream decode line-by-line directly from HTTP body and persist each embedding immediately (or small bounded batches); test with very large generated JSONL lines through `httptest` server.
- **Confidence:** High

#### 3) `embed-status --wait` ignores cancellation/timeout while sleeping
- **Severity:** Medium
- **Evidence:** poll loop uses unconditional `time.Sleep(time.Duration(embedPollSecs) * time.Second)` and never checks command context cancellation in loop body. Command is entry-reachable via `rootCmd.AddCommand(embedCmd)` (`snipe refs embedCmd`).
- **Mechanism:** command can remain blocked up to poll interval (or longer over repeated loops) after interrupt/timeout signal.
- **Failure scenario:** automation sends SIGTERM/timeout expecting fast shutdown; process lingers sleeping, delaying job teardown and causing orchestration timeouts.
- **Minimal fix + tests:** use `select { case <-time.After(...): case <-GetContext().Done(): return GetContext().Err() }`; add test with short poll and canceled context expecting prompt exit.
- **Confidence:** Medium-High

## PASS 2 — Concurrency & Lifecycle Audit

### Concurrency Roots Inventory
- Root pre-run creates signal goroutine that cancels `cmdCtx`.
- `watch` command runs infinite select loop over fsnotify channels + debounce timer.
- `watch` reindex path runs subprocess via `cmd.Run()`.
- Batch polling loops in `embed-status --wait`.

### Findings

#### 1) `watch` command has no `ctx.Done()` shutdown path
- **Severity:** Medium
- **Evidence:** main select in `runWatch` only handles watcher events/errors and timer channel; no context case. Command is reachable from CLI registration (`rootCmd.AddCommand(watchCmd)`, confirmed by `snipe refs watchCmd`).
- **Mechanism:** root signal cancellation is not observed by loop; lifecycle start/stop is asymmetric.
- **Timeline scenario:** operator Ctrl+C triggers root cancel, but watch loop keeps running until watcher channels close externally; process may appear hung.
- **Minimal fix:** include `case <-GetContext().Done(): return GetContext().Err()` in watch select.
- **Test strategy:** command test that cancels context and asserts watch exits quickly.
- **Confidence:** Medium

#### 2) Debounce timer reset without stop/drain can trigger spurious immediate reindex
- **Severity:** Low-Medium
- **Evidence:** repeated `debounceTimer.Reset(...)` in event case with no `Stop()/drain` handling around already-fired timer states.
- **Mechanism:** race in timer semantics can allow stale tick to be consumed after reset pattern, causing extra/full reindex churn.
- **Timeline scenario:** bursty file events produce duplicate reindex invocations, increasing CPU/IO and lock contention.
- **Minimal fix:** guard reset with `if !debounceTimer.Stop() { drain if needed }` before reset.
- **Test strategy:** deterministic timer test with synthetic event burst and count of `runReindex` invocations.
- **Confidence:** Medium

## PASS 3 — Persistence & Boundary Audit

### Boundary Inventory
- **SQLite writes:** `runIndex` → `Store.WriteIndex/WriteImports/WriteFiles/SetMeta`; embedding persistence via `SaveEmbedding`.
- **HTTP boundaries:** Voyage sync embeddings + batch file upload/status/download.
- **File boundaries:** `.snipe/batch_state.json`, `.snipe/embeddings.jsonl`, lockfiles.

### Findings

#### 1) Missing `rows.Err()` check in implementer candidate scan can silently drop results
- **Severity:** Medium
- **Evidence:** in `FindImplementers`, candidate scan loop closes `candRows` but does not check `candRows.Err()` before using `candidates`. Reachability: `runImpl` → `query.FindImplementers` (`snipe callers FindImplementers`).
- **Mechanism:** iteration I/O/decode errors after partial `Next()` traversal are ignored; command returns incomplete implementer set as if successful.
- **Failure scenario:** transient SQLite read issue during long candidate scan returns partial data, misleading users/agents about type coverage.
- **Minimal fix + tests:** after loop add `if err := candRows.Err(); err != nil { ... }`; simulate row iteration error using mocked driver or fault-injection DB wrapper.
- **Confidence:** Medium

#### 2) Scanner token cap (1MB) can fail on oversized batch lines and abort full import
- **Severity:** Medium
- **Evidence:** `ParseBatchResults` sets `scanner.Buffer(buf, 1024*1024)`; any line over 1MB triggers `bufio.ErrTooLong`, returning `scan file` error and failing import.
- **Mechanism:** hard ceiling on boundary payload size with no fallback streaming decoder.
- **Failure scenario:** unusually long symbol text or provider response inflation makes one JSONL line exceed 1MB; entire embedding download fails even though most records are valid.
- **Minimal fix + tests:** raise cap substantially or switch to streaming `json.Decoder` over newline framing; add test with >1MB line confirming robust parsing behavior.
- **Confidence:** Medium
