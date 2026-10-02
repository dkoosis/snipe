# `snipe risk --format json` — stable contract

`snipe risk <base> [head] --format json` hands over raw measures of the diff
between two git refs: what changed, who calls it, who imports it, how often it
changes, and the roles it plays. It gives no score, weights, thresholds or
verdict. What the numbers mean for review is the caller's decision, made on
evidence snipe does not have (sn-qtjl.4).

Callers: sdlc's review routing (`cmd/sdlc/review.go`) reads the JSON;
cc-plugins' dispatch probe (`plugins/dispatch/scripts/lib.sh`) reads the
concise text and drops it when it contains `degraded:`.

The fields below are a **semver-guarded contract**: a rename or a path change is
a **major** version bump. The guard test `test/blackbox/risk_contract_test.go`
fails on any breaking change.

## Shape

Standard snipe envelope; consumers read `.results[0]`:

```json
{
  "protocol": 1,
  "ok": true,
  "results": [
    {
      "changed": { "files": 12, "go_files": 12, "symbols": 22 },
      "callers": 16,
      "caller_files": 6,
      "importers": 0,
      "commits": 32,
      "roles": { "entry_point": 2, "internal": 20 },
      "risk_flags": { "concurrency": 1 },
      "degraded": false
    }
  ],
  "meta": { "command": "risk", "total": 1, "index_state": "fresh", "ms": 12, "…": "…" },
  "error": null
}
```

## Guaranteed fields — `.results[0]`

| Path | Type | Meaning |
|------|------|---------|
| `.changed` | object | `{files, go_files, symbols}` — the diff's shape. Symbols are production symbols whose body a changed line touches; `_test.go` edits add files, not symbols. |
| `.callers` | int | Call sites of the changed symbols (capped at 500). |
| `.caller_files` | int | Files those call sites are in. |
| `.importers` | int | Packages importing a changed package — the most for any one changed package. |
| `.commits` | int | Non-merge commits to a changed Go file over its history — the most for any one file. |
| `.roles` | object | Changed symbols per architectural role (`persistence`, `api_boundary`, `entry_point`, `handler`, `io_primitive`, `factory`, `internal`). May be `{}`. |
| `.risk_flags` | object | Changed symbols per risk flag (`concurrency`, `security_boundary`, …). May be `{}`. |
| `.degraded` | bool | See below — the load-bearing discriminator. |
| `.note` | string | Present (omitempty) only when `degraded`; says why. |

## Invariants

1. **Always exactly one result.** `.results` has length 1 and `.meta.total == 1`
   on every path. Read `.results[0]` unconditionally.
2. **`risk` never fails.** Exit 0 even with no index, a non-git tree, or an
   unresolved ref — those degrade rather than erroring.
3. **`.meta.index_state` is never empty.** `fresh`, `stale` (the index no
   longer matches the code on disk) or `missing`.

## `degraded` — measured vs. not

- **`degraded: false`** — snipe measured a real Go diff. The measures can be
  trusted, including zeros: a `_test.go`-only change measures zero callers.
- **`degraded: true`** — snipe **could not measure**. The measures are zeros
  (except `changed`, when git could read the diff), not evidence of a small
  change. `.note` says why:
  - index absent (`"index unavailable: …"`)
  - index stale (`"index is stale: … — run: snipe index"`) — go.mod, go.sum,
    go.work or go env changed since it was built, or files changed that the
    inline heal did not repair (more than 20, heal disabled, or heal failed).
  - git diff unavailable — not a work tree, unresolved ref, or git absent
  - no changed Go files (`"no changed Go files"`)

Zeros look the same in both cases; `degraded` is the only sound discriminator.
