sha: 4ab5b2c | qa: pass
updated: 2026-02-26

## Do next

Pick from backlog. Top candidates:
- #101 batch self-healing
- #100 exported standalone (bug)
- #88 M4 distribution

## Done

- #103 doctor normalized to standard envelope with error codes + remediation (e3c5b79, 4ab5b2c)
- #93 already closed (73225ba) — cascade exact/suffix/substring in place

## Backlog

#88 M4 distribution | #100 exported standalone | #101 batch self-heal | orca persistToolCall

## Traps

- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Enrichment pipeline is roadmap not abandoned — plan at `docs/PLAN-context-enrichment.md`
