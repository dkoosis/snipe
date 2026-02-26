sha: 3f3810a | qa: pass
updated: 2026-02-26

## Do next

Pick from backlog. Top candidate: #88 M4 distribution (goreleaser, zero-config install).

## Done

- mage all/build/qa now use `go install` — binary lands on PATH for all users (3f3810a)
- #101 batch self-healing (bd17813)
- #100 closed as not-a-snipe-bug

## Backlog

#88 M4 distribution | orca persistToolCall

## Traps

- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Enrichment pipeline is roadmap not abandoned — plan at `docs/PLAN-context-enrichment.md`
