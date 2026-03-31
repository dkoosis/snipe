# Session Protocol

How to work on snipe. Stable — only update when the workflow itself changes.

## Startup

1. Read `docs/progress.md` — orient on what's been built
2. Read boot.md "do next" — pick ONE task
3. Run `make` (build + lint + test) to confirm green baseline
4. If red: fix first, that's the session's task

## During

### Workflow

- One task per session. Finish it or document why it's blocked.
- Commit after each logical unit that passes `make`.
- For removals: batch by file/package, `make ci` after each batch.

### Verification evidence

| Claim | Required proof |
|-------|---------------|
| "removed dead code" | `make ci` passes, grep confirms symbol gone |
| "fixed bug" | test covering the fix, before/after shown |
| "improved eval" | eval score before and after, same harness |
| "no behavior change" | blackbox tests pass, diff reviewed |

### Commit style

- Prefix: `feat:`, `fix:`, `refactor:`, `chore:`, `test:`, `docs:`
- One logical change per commit
- If touching orca: separate commit, separate verification

## Shutdown

1. Run `make ci` — must pass
2. Update `docs/progress.md` — mark completed tasks, add notes
3. Rewrite `boot.md` — new SHA, new "do next", move completions to "done"
4. If eval was affected: run eval, record score in progress.md
5. Commit the PM file updates: `chore: wrap session — <what changed>`

## Integration with orca

- snipe is a subprocess dependency of orca's `go_symbol()` MCP tool
- Changes to snipe's JSON envelope or command interface require orca-side verification
- `--caller`/`--request-id` flags are reserved for orca telemetry — don't delete
- Test integration: `test/bench/orca_test.go` (requires orca at `../orca`)

## Constraints

- Do NOT optimize eval score until telemetry provides ground truth
- Do NOT touch enrichment phases without user approval (see `docs/PLAN-context-enrichment.md`)
- `make ci` is the merge gate — no exceptions
- Output: Claude-optimized by default (D1, D4). JSON envelope available via `--format json` for orca integration
