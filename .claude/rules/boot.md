sha: d3ef979 | qa: pass
updated: 2026-02-26

## Do next

Pick from backlog. Top candidate: #88 M4 distribution (goreleaser, zero-config install).

## Done

- Merged PR #104: 3-pass true-bug audit (6 findings verified — FK reindex, error suppression, negative index panic, watch lifecycle, HTTP context)
- Cleaned all branches, stashes, stale PRs — main is sole branch, zero debris
- Tracked session-protocol.md, refreshed baselines

## Backlog

#88 M4 distribution | #96 batch embed RAM | #102 callees meta.self | orca persistToolCall

## Traps

- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Enrichment pipeline is roadmap not abandoned — plan at `docs/PLAN-context-enrichment.md`
- Audit findings at `docs/audits/2026-02-26-true-bug-audit.md` — FK + error suppression are high-severity
