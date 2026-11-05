# Session Protocol

How to work on snipe. Stable — only update when the workflow itself changes.

## Startup

1. Read `docs/progress.md` — orient on what's been built
2. Read the sdlc banner ("Last time", "Next") and run `bd ready` — pick ONE task
3. Run `make` (build + lint + test) to confirm green baseline
4. If red: fix first, that's the session's task

## During

### Workflow

- One task per session. Finish it or document why it's blocked.
- Commit after each logical unit that passes `make`.
- For removals: batch by file/package, `make audit` after each batch.
- ‡ **Batch small fixes → one PR.** Several small/independent fixes in flight → ONE branch, ONE PR, one commit per bead. `make audit`/CI fires once at the PR (+ once on merge to main), NOT once per fix — a PR-per-one-liner serializes the queue behind build time. Each fix stays its own commit (traceable); PR body lists them. Bundle by session/theme; ✗ mix a risky change in with trivial ones (it drags the whole PR's review bar up). **Default: auto-batch** — ≥2 small fixes queued → roll them onto one PR without asking.
- ‡ **PR ↔ beads.** Every PR body carries a `Closes:` trailer naming the beads it lands (`Closes: snipe-abc, snipe-def`; no bead → `Closes: none`); squash-merge keeps it in main's commit. On merge, close them with the ref: `bd close <ids> --reason "merged #<PR> (<sha>)"`. ✗ merge-then-forget — a landed-but-open bead is a leak.

### Verification evidence

| Claim | Required proof |
|-------|---------------|
| "removed dead code" | `make audit` passes, grep confirms symbol gone |
| "fixed bug" | test covering the fix, before/after shown |
| "improved eval" | eval score before and after, same harness |
| "no behavior change" | blackbox tests pass, diff reviewed |

### Commit style

- Prefix: `feat:`, `fix:`, `refactor:`, `chore:`, `test:`, `docs:`
- One logical change per commit

### Test traps

- ‡ Git-fixture tests set `core.hooksPath=/dev/null`; keep it on any new throwaway-repo test, or the global commit-msg hook rejects fixture commits.
- ‡ Tests that exec the `snipe` binary set `KEYRING_DISABLE`, or an unlocked-keychain `/usr/bin/security` exec can prompt or hang the suite.
- ‡ Blackbox `run()` must not pass `VOYAGE_API_KEY` through to `snipe index` (#286), or the suite submits real Voyage batches.

## Shutdown

1. Run `make audit` — must pass
2. Update `docs/progress.md` — mark completed tasks, add notes
3. If eval was affected: run eval, record score in progress.md
4. Commit the PM file updates: `chore: wrap session — <what changed>`

## Constraints

- Do NOT optimize eval score until telemetry provides ground truth
- Do NOT touch enrichment phases without user approval (see `docs/PLAN-context-enrichment.md`)
- `make audit` is the merge gate — no exceptions
- Output: Claude-optimized by default (D1, D4). JSON envelope available via `--format json` for toolchain integration
