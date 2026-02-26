sha: bd17813 | qa: pass
updated: 2026-02-26

## Do next

Pick from backlog. Top candidate: #88 M4 distribution (goreleaser, zero-config install).

## Done

- #101 batch self-healing: completed-but-unprocessed batches auto-recover instead of requiring manual file deletion (bd17813)
- #100 closed as not-a-snipe-bug — standalone exported funcs correctly categorized in snipe's own index

## Backlog

#88 M4 distribution | orca persistToolCall

## Traps

- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Enrichment pipeline is roadmap not abandoned — plan at `docs/PLAN-context-enrichment.md`
