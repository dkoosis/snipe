sha: 48f2cd6 | qa: pass
updated: 2026-02-26

## Do next

Pick from backlog. Top candidates:
- #93 wildcard predicates
- #101 batch self-healing
- #103 doctor schema normalization
- #88 M4 distribution

## Done

- #102 callees output contract fixed — ID/file/range now coherent, defResult prepend removed (d8bf6fa)
- Extracted `CallRow.ToCalleeResult()`/`ToCallerResult()` — deduplicated 8 conversion blocks, -130 lines (48f2cd6)
- Blackbox test strengthened: 16-char hex ID, real kind, definition range, meta.total, ID chain via `snipe show`

## Backlog

#93 wildcard predicates | #88 M4 distribution | #100 exported standalone | #101 batch self-heal | #103 doctor schema | orca persistToolCall

## Traps

- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Enrichment pipeline is roadmap not abandoned — plan at `docs/PLAN-context-enrichment.md`
