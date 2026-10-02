# `snipe risk --format json` — stable contract

`snipe risk <base> [head] --format json` hands a review router the measures that
bear on how much review a diff needs, grouped into five factors, each scored 0-1,
and one overall score from visible, versioned weights. There is no band or
verdict: the router picks its own cut-offs, and the weights get tuned as outcomes
show which factors matter (sn-qtjl.4).

## Why a score and its contributors

- **The score is a simple start.** A router can route on one number without
  knowing what is behind it.
- **The factors, weights and measures let it improve.** Nobody yet knows which
  of these predict review trouble. With the inputs and weights visible, when
  outcomes show that one factor matters a lot and another not at all, the
  weights change in one obvious place. A router can also route on a factor
  directly.
- **No bands.** low/medium/high presumes how many routing steps there are and
  where the cut-offs sit. Those are the router's decisions.
- **Missing is not safe.** A null score means snipe could not measure; a factor
  with no data is left out, not counted as 0.

The score is rough, not precise: use it to rank and route, and expect the
weights to change as evidence comes in.

The factors, in plain words:

| Factor | Question it answers |
|--------|--------------------|
| reach | How far could a mistake spread? |
| difficulty | How hard is the changed code to get right? |
| history | Has this code needed fixing, and does it churn? |
| kind | Does it touch persistence, concurrency, security or exported API? |
| safety_net | How much of the changed code would no test catch? |

Callers: sdlc's review routing (`cmd/sdlc/review.go`) reads the JSON;
cc-plugins' dispatch probe (`plugins/dispatch/scripts/lib.sh`) reads the
concise text and drops it when it contains `degraded:`.

The fields below are a **semver-guarded contract**: a rename or a path change is
a **major** version bump. The guard test `test/blackbox/risk_contract_test.go`
fails on any breaking change. A change to the weights or curve midpoints is not a
contract change; it bumps `weights_version`.

## Shape

Standard snipe envelope; consumers read `.results[0]`:

```json
{
  "score": 0.351,
  "factors": { "reach": 0.222, "difficulty": 0.557, "history": 0.38, "kind": 0.3, "safety_net": 0.294 },
  "weights": { "reach": 1, "difficulty": 1, "history": 1, "kind": 1, "safety_net": 1 },
  "weights_version": 1,
  "changed": { "files": 12, "go_files": 12, "test_files": 5, "symbols": 22, "code_lines_added": 253, "code_lines_removed": 42 },
  "reach": { "callers": 16, "caller_files": 6, "importers": 0, "pkg_rank": null, "pkg_count": 0 },
  "difficulty": { "cyclo_max": 14, "cognitive_max": 17 },
  "history": { "commits": 32, "bug_commits": 0, "churn": 31.8, "authors": 1 },
  "kind": { "roles": { "entry_point": 2, "internal": 20 }, "risk_flags": { "concurrency": 1 }, "exported": 0 },
  "tests": { "changed_funcs": 17, "untested": ["sdlc.Merge.run", "…"] },
  "focus": [ { "name": "sdlc.Ship.run", "file": "cmd/sdlc/ship.go", "line": 19, "callers": 1, "cyclo": 14, "cognitive": 14, "tested": false } ],
  "degraded": false
}
```

`.meta.index_state` is `fresh`, `stale` or `missing`, never empty.

## Score and factors

| Path | Meaning |
|------|---------|
| `.score` | Weighted mean of the factors present, 0-1. **null when degraded.** |
| `.factors` | Each factor 0-1. A factor whose source is missing (no changed functions, no churn in the index, no changed functions to test) is **absent**, not 0. |
| `.weights`, `.weights_version` | The weights that produced `.score`. Start equal (v1). |

Each measure maps to 0-1 by `x / (x + mid)`, which is 0.5 at its midpoint; a
factor is the mean of its measures. Midpoints (v1): callers 20, importers 5,
cyclomatic 10, cognitive 15, bug commits 5, churn 10, risk-flagged symbols 1,
non-internal-role symbols 3, exported symbols 3.

| Factor | Measures |
|--------|----------|
| reach | callers, importers, package PageRank position (rank 1 of N → 1) |
| difficulty | cyclomatic and cognitive complexity, the most for any changed function |
| history | bug-fix commits and recency-weighted churn, the most for any changed file |
| kind | changed symbols with a risk flag; changed symbols in a non-`internal` role; changed exported symbols |
| safety_net | share of changed functions that no test reaches |

## Measures

| Path | Meaning |
|------|---------|
| `.changed` | Size: files, Go files, test files, production symbols touched, code lines added/removed (non-test Go). A measure, not a factor. |
| `.reach.callers`, `.caller_files` | Call sites of the changed symbols (capped at 500), and their files. |
| `.reach.importers` | Packages importing a changed package — the most for any one. |
| `.reach.pkg_rank`, `.pkg_count` | PageRank rank of the most central changed package, of how many; `pkg_rank` null when not computed (incremental index) or no changed package is ranked. |
| `.difficulty.cyclo_max`, `.cognitive_max` | Complexity of the changed functions, measured on the functions themselves at head; null when no function changed. |
| `.history` | `commits`, `bug_commits` (Bead-Type: bug trailer), `churn` (180-day half-life), `authors` — the most for any changed Go file. **null when the index holds no churn.** |
| `.kind.roles`, `.risk_flags` | Changed symbols per role / risk flag. |
| `.kind.exported` | Changed exported symbols. |
| `.tests.changed_funcs`, `.untested` | Changed functions, and the ones no test reaches. |
| `.focus` | Up to 3 changed functions to look at first: most direct callers, then most complex. |
| `.degraded`, `.note` | See below. |

## Invariants

1. **Always exactly one result.** `.results` has length 1 and `.meta.total == 1`
   on every path. Read `.results[0]` unconditionally.
2. **`risk` never fails.** Exit 0 even with no index, a non-git tree, or an
   unresolved ref — those degrade rather than erroring.
3. **`.meta.index_state` is never empty.**
4. **Degraded means no score.** `.score` is null whenever `.degraded` is true.

## `degraded` — measured vs. not

- **`degraded: false`** — snipe measured a real Go diff. The score and
  measures can be trusted, including a low score.
- **`degraded: true`** — snipe **could not measure**; `.score` is null. `.note`
  says why:
  - index absent (`"index unavailable: …"`)
  - index stale (`"index is stale: … — run: snipe index"`) — go.mod, go.sum,
    go.work or go env changed since it was built, or files changed that the
    inline heal did not repair (more than 20, heal disabled, or heal failed).
  - git diff unavailable — not a work tree, unresolved ref, or git absent
  - no changed Go files (`"no changed Go files"`)
