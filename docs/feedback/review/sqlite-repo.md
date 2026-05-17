---
review_date: 2026-05-17
driver: modernc.org/sqlite v1.44.0
modules_analyzed: github.com/dkoosis/snipe (single module)
severity:
  critical: 0
  important: 2
  moderate: 4
  optional: 3
run_id: ecebe5258308
---

# SQLite review — snipe

## 1. Executive summary

snipe runs a single-writer embedded SQLite index (`.snipe/index.db`) under `modernc.org/sqlite`, driven through `database/sql`. The connection-scope hazard that usually dominates this rubric is sidestepped by an explicit `db.SetMaxOpenConns(1)` plus the verified-PRAGMA pattern in `internal/store/store.go` — every PRAGMA is set and then read back, and there is only ever one pooled connection. WAL, `synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON`, and `temp_store=MEMORY` are all applied. Integrity check is parsed correctly (`!= "ok"` is fatal).

The real soft spots are: (a) `db.Begin()` everywhere with deferred lock acquisition under WAL, (b) error classification via `strings.Contains` against driver text, (c) no `Context` propagation into query/lifecycle hot paths, (d) no down migrations exist (forward-only migration table), and (e) a FK-OFF window around incremental reindex with no `foreign_key_check` between rebuild and re-enable.

Overall tier: **3 / 4** — production-acceptable for a single-process indexer; would not survive promotion to a multi-writer or server workload without P1 fixes.

## 2. SQLite surface area map

| Area | File | Function | DB | Notes |
|---|---|---|---|---|
| Main store open | `internal/store/store.go:29` | `Open` | `.snipe/index.db` | All PRAGMAs set + verified, `MaxOpenConns=1` |
| Schema migrations | `internal/store/schema.go` | `initSchema`, `runMigration` | same | 17 forward-only migrations, version recorded in `meta` + `migrations` table inside same tx |
| Bulk write tx | `internal/store/write.go:38,332,378,473,671,740,760` | `Write*`, `Incremental*` | same | Each opens its own `s.db.Begin()` (no `BeginTx`/no Immediate) |
| Incremental FK window | `internal/store/write.go:466-471` | `*incrementalUpdate*` | same | `PRAGMA foreign_keys=OFF` on a Conn before tx; ON via deferred Exec; no `foreign_key_check` |
| Graph metrics tx | `internal/store/metrics.go:35`, `sccs.go:17` | metric/SCCs writers | same | `s.db.Begin()` |
| Integrity check | `cmd/doctor.go:207` | `checkIndex` | same | `PRAGMA integrity_check`, value compared `!= "ok"` |
| Recursive CTE | none (non-vendor) | — | — | `WITH RECURSIVE` not used; `impact`/`tests` use bounded 1+2-hop CTEs |
| Process lock | `internal/store/store.go:139` | `AcquireLock` | `.lock` file | PID-based, stale-lock race fixed via re-read-and-match |

Test code uses `:memory:` extensively; out of scope for the rubric (`don't flag in-memory DBs`).

## 3. Highest-risk findings

1. `db.Begin()` (deferred BEGIN) used in every write path — surfaces `SQLITE_BUSY` mid-tx under any future contention. Today the `MaxOpenConns=1` constraint hides this; the moment a second connection or process is added (orca subprocess, parallel indexer) it bites.
2. Error classification by substring match in `isNoSuchTableErr` (`internal/store/write.go:663`) — driver text contract, not a typed result-code check.
3. No `Context` propagated to any `db.Query`/`db.Exec` outside `incrementalUpdate` — caller cancellation (e.g. orca request timeout) cannot interrupt a large `impact`/`pack_package` query.
4. Migration system is forward-only — no `.down.sql`, no programmatic reversal. Acceptable but undocumented; CI cannot verify `Up → Down → Up`.
5. Incremental indexer disables FKs for a transaction window without running `PRAGMA foreign_key_check` before re-enabling them — invariant violations silently persist.

## 4. Findings

### Finding 1 — Implicit deferred transaction strategy on every writer
- **File:** `internal/store/write.go:38,332,378,671,740,760`; `internal/store/schema.go:327`; `internal/store/sccs.go:17`; `internal/store/metrics.go:35`
- **Function/Query:** `WriteBatch`, `WritePackageDocs`, `WriteStringRefs`, `WriteGraphMetrics`, `runMigration`, etc.
- **Severity:** Important
- **Confidence:** high
- **Category:** txn
- **Evidence:**
  ```go
  tx, err := s.db.Begin()              // → BEGIN (deferred)
  if err != nil { ... }
  defer func() { rollbackOnError(tx, &err) }()
  // ... many INSERT/DELETE statements ...
  return tx.Commit()
  ```
- **Why it matters:** Plain `BEGIN` acquires the write lock at the first write, not at BEGIN. Under WAL with any concurrent writer (today: a second `snipe index` process; tomorrow: orca calling in parallel), `SQLITE_BUSY` can surface mid-transaction after several statements have already run. `BEGIN IMMEDIATE` fails fast and predictably. `MaxOpenConns=1` only blocks contention from one Go process; OS-level multi-process contention against `.snipe/index.db` is not prevented (`AcquireLock` is advisory, files can still be opened).
- **Recommended fix:** Add a `beginImmediate(s *Store) (*sql.Tx, error)` helper that does `db.Exec("BEGIN IMMEDIATE")` on a Conn (or use `modernc.org/sqlite`'s `_txlock=immediate` DSN parameter on the writer). Apply to all writer paths; leave migrations as-is (single boot-time writer).
- **Validation:** Spawn two `snipe index` processes against the same repo; verify the loser surfaces BUSY immediately, not after partial writes.

### Finding 2 — Error classification by substring
- **File:** `internal/store/write.go:658-664`
- **Function/Query:** `isNoSuchTableErr`
- **Severity:** Important
- **Confidence:** high
- **Category:** error
- **Evidence:**
  ```go
  func isNoSuchTableErr(err error, table string) bool {
      var sqliteErr *sqlite.Error
      if !errors.As(err, &sqliteErr) {
          return false
      }
      return strings.Contains(strings.ToLower(sqliteErr.Error()),
          "no such table: "+strings.ToLower(table))
  }
  ```
- **Why it matters:** `modernc.org/sqlite` exposes typed extended result codes via `(*sqlite.Error).Code()` — the schema-mismatch case is `SQLITE_ERROR` (1) with a parseable structure, and the better idiom for "is this table missing" is `SELECT 1 FROM sqlite_master WHERE type='table' AND name=?` once on store open, cached in a `map[string]bool`. The current code is locale-fragile and silently breaks if the driver ever rewords the message.
- **Recommended fix:** Cache `existingTables` at `Store` init (one `SELECT name FROM sqlite_master WHERE type='table'`); change call sites from "try then swallow" to "check then skip". Removes the string match entirely.
- **Validation:** Unit-test the cache against a freshly-migrated DB and against a v1 DB before `embeddings`/`symbol_purposes` exist.

### Finding 3 — Context never reaches the DB in read paths
- **File:** `internal/query/lookup.go` (16 sites), `internal/query/impact.go:108,143,206,208`, `internal/query/tests.go:96,98,181`, `internal/query/state.go:139`, `internal/query/deps.go`, `cmd/lifecycle.go:347`, `cmd/boundary.go:133`, `cmd/pack_package.go:255`, `internal/store/embed.go:50,93,138`, `internal/store/sccs.go:56`, `internal/store/metrics.go:76,82`
- **Function/Query:** all `db.Query(...)` / `db.Exec(...)` (~30 sites)
- **Severity:** Moderate
- **Confidence:** high
- **Category:** safety
- **Evidence:**
  ```go
  rows, err := db.Query(q, args...)    // no ctx
  ```
  vs the one place that does it right:
  ```go
  conn.ExecContext(context.Background(), "PRAGMA foreign_keys=OFF") // internal/store/write.go:466
  ```
- **Why it matters:** snipe is documented as a subprocess of orca's `go_symbol` MCP tool. When the upstream tool cancels (`--request-id` cancellation, client disconnect, parent timeout), the running query keeps consuming CPU and a connection until completion. For `pack_package`, `impact`, and `lifecycle` against large repos this can be many hundreds of ms.
- **Recommended fix:** Thread `context.Context` from each cobra command down through `internal/query/*` and convert to `QueryContext`/`ExecContext`. Cobra already provides `cmd.Context()`.
- **Validation:** Add a test that cancels `ctx` mid-`impact` and asserts the call returns `ctx.Err()` instead of a full result set.

### Finding 4 — Migrations are forward-only and undocumented as such
- **File:** `internal/store/schema.go:20-266`
- **Function/Query:** `migrations` table
- **Severity:** Moderate
- **Confidence:** high
- **Category:** migration
- **Evidence:** `migration` struct has `version`, `name`, `up` — no `down` field. No `.down.sql`. No comment stating the policy.
- **Why it matters:** Each up migration uses `CREATE INDEX IF NOT EXISTS` / `CREATE TABLE IF NOT EXISTS` / `ADD COLUMN`, which is fine for forward replay against fresh or already-stamped DBs. But there is no path to roll back a buggy migration without `rm .snipe/index.db && snipe index` — for a developer index this is fine; the policy just needs to be written down so future contributors don't assume reversibility exists.
- **Recommended fix:** Add a `// Policy: migrations are forward-only. Roll back by deleting .snipe/ and reindexing.` comment near the `migrations` slice. Optionally add a `bd remember` entry. No code change required.
- **Validation:** N/A — documentation finding.

### Finding 5 — FK-OFF window has no `foreign_key_check`
- **File:** `internal/store/write.go:466-471`
- **Function/Query:** incremental update path
- **Severity:** Moderate
- **Confidence:** medium
- **Category:** integrity
- **Evidence:**
  ```go
  if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil { ... }
  defer func() {
      _, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
  }()
  tx, err := conn.BeginTx(ctx, nil)
  // ... delete refs / call_graph / imports / symbols, insert new ...
  ```
- **Why it matters:** During an incremental reindex of a changed file, refs/call_graph rows may transiently reference symbols that were just deleted but not yet rewritten. With FKs off this is fine; the risk is a bug inside the rewrite path leaving the table genuinely inconsistent, which would only surface much later when FKs are checked on a *different* statement. Running `PRAGMA foreign_key_check` after the rewrite and before re-enabling catches that immediately.
- **Recommended fix:** Inside the deferred re-enable, before turning FKs back on, run `PRAGMA foreign_key_check` and fail the indexer (or log a SEV warning) if it returns any rows.
- **Validation:** Inject a bug that leaves an orphan `call_graph.callee_id` and assert the checker catches it.

### Finding 6 — Verified PRAGMAs ignore that `foreign_keys` is connection-local even at pool size 1
- **File:** `internal/store/store.go:82-87`
- **Function/Query:** `Open`
- **Severity:** Moderate
- **Confidence:** medium
- **Category:** pragma
- **Evidence:** `db.SetMaxOpenConns(1)` + `db.SetMaxIdleConns(1)` at lines 90-91 — but `SetConnMaxLifetime` is **not** set, so `database/sql` may at some point recycle the single idle conn and the new conn will boot **without** `foreign_keys=ON`. The first `db.Exec("PRAGMA foreign_keys=ON")` only affects the connection then bound to the pool.
- **Why it matters:** Today the default `ConnMaxLifetime=0` means conns are never proactively recycled, so the verified PRAGMAs do hold. If `SetConnMaxLifetime(N)` ever gets added, FK enforcement silently drops on a recycled conn. The idiomatic fix here is the standard one: register a driver wrapper with a `ConnectHook` (modernc exposes `sqlite.RegisterFunction` but not a public `ConnectHook` — alternatively, append PRAGMAs to the DSN: `file:.snipe/index.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)`).
- **Recommended fix:** Move connection-local PRAGMAs into the DSN (modernc supports `_pragma=...`) so they apply on *every* connection the driver opens, then drop the post-open `db.Exec("PRAGMA ...")` calls but keep the verify step. Leaves `journal_mode` (database-wide, persistent) and `synchronous` (database-wide, persistent) once on open, and `foreign_keys`/`busy_timeout`/`temp_store` per-conn via DSN.
- **Validation:** Set `SetConnMaxLifetime(10ms)` in a test, force a recycle, and assert `PRAGMA foreign_keys` still reports 1.

### Finding 7 — `Sscanf` errcheck swallow on incremental counter
- **File:** `internal/store/write.go:455`
- **Function/Query:** incremental counter parse
- **Severity:** Optional
- **Confidence:** high
- **Category:** safety
- **Evidence:**
  ```go
  fmt.Sscanf(countStr, "%d", &incCount) //nolint:errcheck // best-effort parse
  ```
- **Why it matters:** If the `incremental_count` meta row ever holds a non-integer (manual edit, future schema rename), `incCount` stays 0 and the next reindex silently misbehaves. Not a SQLite bug per se but the value lives in the SQLite `meta` table and the lint suppression hides a real schema-validity assumption.
- **Recommended fix:** Use `strconv.Atoi` and treat parse failure as "force full reindex" rather than silently zeroing.
- **Validation:** Set `meta.incremental_count = 'abc'`, run incremental, assert behavior matches the docs.

### Finding 8 — `pkg_path LIKE '%/...'` and `pkg_path LIKE ? || '/%'` on every importer/depender query
- **File:** `internal/query/deps.go:36,37,73,85,122`; `internal/query/lookup.go:248,1100,1101`; `internal/query/resolve.go:55`
- **Function/Query:** various
- **Severity:** Optional
- **Confidence:** medium
- **Category:** index
- **Evidence:**
  ```sql
  WHERE pkg_path NOT LIKE '%/internal/%'
    AND pkg_path NOT LIKE '%/cmd/%'
  -- and
  WHERE importer_pkg LIKE ? || '%' AND pkg_path LIKE ? || '/%'
  ```
- **Why it matters:** Leading-wildcard `LIKE '%...'` defeats the index on `pkg_path`. The `imports` table can grow into the tens of thousands of rows on a large repo. Today snipe targets repos in the hundreds-of-thousands-of-symbols range, so `imports` is bounded and this is acceptable. Flag-only; do not act unless eval shows it on the hot path.
- **Recommended fix:** None unless `snipe metrics --kind cycles` or `deps` shows up in the eval latency budget. If it does: store an inverted `pkg_path_segments` table or use the `glob` operator on prefix-only matches.
- **Validation:** `EXPLAIN QUERY PLAN` on the largest snipe-targeted repo; if it shows `SCAN imports`, revisit.

### Finding 9 — `SELECT *` in transitive CTE projection
- **File:** `internal/query/impact.go:100,196`; `internal/query/tests.go:86,173`
- **Function/Query:** `FindImpactCallers`, test discovery CTEs
- **Severity:** Optional
- **Confidence:** high
- **Category:** perf
- **Evidence:** `SELECT * FROM direct_callers UNION ALL SELECT * FROM transitive_callers`
- **Why it matters:** These `SELECT *`s read from CTEs the same file defines two lines above with an explicit column list, so the schema-drift risk is zero (the CTE column list is right there). Stylistically it's still worth tightening because anyone changing the CTE projection silently changes downstream `scanTestRows` row width.
- **Recommended fix:** Spell the column list once in a `const callerCols = "id, name, kind, ..."` and reuse. Mechanical.
- **Validation:** Existing tests cover the row shape.

### Finding 10 — Integrity check is correct but only triggered by `snipe doctor`
- **File:** `cmd/doctor.go:207`
- **Function/Query:** `checkIndex`
- **Severity:** Optional
- **Confidence:** high
- **Category:** integrity
- **Evidence:** `PRAGMA integrity_check` is run, `!= "ok"` is treated as corrupt, message wired to remediation. Good.
- **Why it matters:** Not a defect; noting that `Store.Open` does **not** run a quick_check at startup. For a developer index where corruption is rare and re-indexing is cheap this is fine. If a future user reports a confusing query failure the symptom may be silent corruption that `snipe doctor` would have caught.
- **Recommended fix:** No code change. Optionally add a `bd remember` note pointing users at `snipe doctor` when query results look impossible.
- **Validation:** N/A.

## 5. Recommended diffs (sketch)

- **Finding 6 (DSN PRAGMAs):**
  ```go
  // store.go
  const dsnSuffix = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=temp_store(2)"
  db, err := sql.Open("sqlite", path + dsnSuffix)
  // drop the connection-local db.Exec("PRAGMA foreign_keys=ON") etc; keep verify
  ```
- **Finding 1 (BEGIN IMMEDIATE):**
  ```go
  func (s *Store) beginWrite(ctx context.Context) (*sql.Tx, error) {
      conn, err := s.db.Conn(ctx)
      if err != nil { return nil, err }
      if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
          conn.Close(); return nil, err
      }
      // wrap conn in a tx-like type, or use db.BeginTx with driver-specific opts
      ...
  }
  ```
- **Finding 5 (`foreign_key_check`):**
  ```go
  defer func() {
      if rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check"); err == nil {
          defer rows.Close()
          if rows.Next() { /* corruption — log and refuse to re-enable */ }
      }
      _, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
  }()
  ```

## 6. SQL index and migration changes

None recommended. The existing index coverage matches the observed query patterns; no missing index on a hot join surfaced in the audit.

## 7. EXPLAIN / performance validation notes

Not run for this pass. The findings that hinge on plan shape (Finding 8) explicitly call for `EXPLAIN QUERY PLAN` on a representative sibling repo (e.g. `../orca`) before acting.

## 8. Open questions

- Will snipe ever support a second concurrent writer (e.g. orca calling `snipe index` while a user has another shell open)? If yes, Finding 1 graduates to Important. If no, the current `MaxOpenConns=1` + advisory file lock is sufficient and Finding 1 can be downgraded to Optional.
- The `--caller`/`--request-id` reserved flags for orca telemetry imply orca will attach context to each call. Finding 3 (no `Context`) is therefore directly observable from orca's side as un-cancellable subprocess queries.
- Manifest expectation of `ncruces/go-sqlite3` versus actual `modernc.org/sqlite` — noted; rules generalize cleanly.

## 9. Final score

**3 / 4 — production-acceptable.**

PRAGMA discipline is the strongest part of this codebase: every connection-local PRAGMA is set and *verified*, which is rare. The single-writer pool, advisory file lock, and correctly-parsed `integrity_check` carry the rest. The gaps are real (deferred BEGIN under WAL, string-match error classification, no context propagation, FK-OFF without `foreign_key_check`) but each has a small, mechanical fix and none currently produces a known incident — they are pre-positioning for the moment snipe stops being a single-process indexer.
