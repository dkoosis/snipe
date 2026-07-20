# `snipe risk --format json` — stable contract

`snipe risk <base> [head] --format json` emits a code-graph risk verdict for the
diff between two git refs. It is consumed **cross-repo** by the cc-plugins
review-judge (ccp-1cfm), which branches CI review depth on the verdict. The
fields and guarantees below are a **semver-guarded contract**: a rename or a
path change is a **major** version bump. The guard test
`test/blackbox/risk_contract_test.go` (sn-n8re) fails on any breaking change — if
it goes red, a downstream CI judge breaks, so treat a red as intentional only
with a major bump + a heads-up to `@cc-plugins`.

## Shape

Standard snipe envelope; consumers read `.results[0]`:

```json
{
  "protocol": 1,
  "ok": true,
  "results": [
    {
      "verdict": "medium",
      "score": 2,
      "reasons": [
        { "signal": "central", "detail": "…ranks #15/31 by PageRank", "weight": 1 },
        { "signal": "churn-hotspot", "detail": "…in churn top-20", "weight": 1 }
      ],
      "changed": { "files": 2, "go_files": 2, "symbols": 3 },
      "degraded": false
    }
  ],
  "meta": { "command": "risk", "total": 1, "ms": 12, "…": "…" },
  "error": null
}
```

## Guaranteed fields — `.results[0]`

| Path | Type | Meaning |
|------|------|---------|
| `.verdict` | string | `low` \| `medium` \| `high`. Forced `low` when `degraded`. |
| `.score` | int | Summed signal weights (deduped per signal class). |
| `.reasons` | array | Fired signals, each `{signal, detail, weight}`. May be `[]`. |
| `.reasons[].signal` | string | Stable signal code (`central`, `churn-hotspot`, `blast`, `role:*`, `risk:*`, `bead-central`). |
| `.changed` | object | `{files, go_files, symbols}` — diff shape (observability, not scored). |
| `.degraded` | bool | See below — the load-bearing discriminator. |
| `.note` | string | Present (omitempty) only when `degraded` or otherwise noteworthy. |

## Invariants

1. **Always exactly one result.** `.results` has length 1 and `.meta.total == 1`
   on every path — clean, degraded, or unanalyzable. `risk` **never** emits
   `results == []`. Read `.results[0]` unconditionally; **do not** branch on
   `len(results)`.
2. **`risk` never fails.** Exit 0 even with no index, a non-git tree, or an
   unresolved ref — those degrade rather than erroring.

## `degraded` — trust vs. fallback

`degraded` is the discriminator a consumer **must** branch on. `len(reasons)`
is **not** a safe proxy: an unanalyzable diff also carries `reasons == []`.

- **`degraded: false`** — snipe analyzed a real Go diff. Trust the verdict. A
  clean diff here reads `verdict: low` with `reasons: []` → the judge's
  `tier:none`.
- **`degraded: true`** — snipe **couldn't analyze**; the verdict is a safe-low
  placeholder, not evidence of low risk. The consumer should drop to its
  portable fallback. `.note` says why. Three cases:
  - index absent (`"index unavailable: …"`)
  - git diff unavailable — not a work tree, unresolved ref, or git absent
  - no changed Go files (`"no changed Go files"`)

The trap: a clean analyzed diff (`degraded:false`, `reasons:[]`) and an
unanalyzable diff (`degraded:true`, `reasons:[]`) can look identical on
`reasons` alone, but the judge branches on them **oppositely**. `degraded` is
the only sound signal.

## Consumer recipe (cc-plugins review-judge)

```
degraded == true                              → portable fallback (don't trust empty as safe)
degraded == false && verdict == "low"         → tier:none  (trust the clean verdict)
degraded == false && verdict in {medium,high} → escalate review depth
```
