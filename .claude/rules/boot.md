sha: a327e37 | qa: pass
updated: 2026-02-26

## Do next

Pick from backlog. Top candidate: #88 M4 distribution (goreleaser, zero-config install).

## Done

- Closed #102 — already fixed in d8bf6fa, verified output has no definition prepend
- Simplified 6 files, removed ~127 lines of duplicate logic (a327e37)
- Merged PR #104: 3-pass true-bug audit with 6 verified findings (ae16960)

## Backlog

#88 M4 distribution | #96 batch embed RAM | orca persistToolCall

## Traps

- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Enrichment pipeline is roadmap not abandoned — plan at `docs/PLAN-context-enrichment.md`
- Audit findings at `docs/audits/2026-02-26-true-bug-audit.md` — FK + error suppression are high-severity
