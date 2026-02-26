sha: 0bdfff9 | qa: pass | eval: 80.8%
updated: 2026-02-25

## Do next

Pick from backlog. Top candidates:
- #93 wildcard predicates
- #101 batch self-healing
- #102 callees output contract
- #103 doctor schema normalization
- #88 M4 distribution

## Done

- Phase 3 simplification complete — all 28 dead symbols removed, enrichment stub gutted, flags hidden
- Fixed mage: scoped gofmt to project dirs, removed contradictory gosec step (9512a0a)
- #94 hash staleness (0c34b4a), #96 batch OOM (0bdfff9), #97 watch shutdown (a776c5a), #98 rows.Err (de71af4), #99 interface dispatch (0f33ada)

## Backlog

#93 wildcard predicates | #88 M4 distribution | #100 exported standalone | #101 batch self-heal | #102 callees contract | #103 doctor schema | orca persistToolCall

## Traps

- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Enrichment pipeline is roadmap not abandoned — plan at `docs/PLAN-context-enrichment.md`
