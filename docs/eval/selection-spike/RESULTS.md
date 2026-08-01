# CLI command-selection spike — sn-l1kh.6

**Question the bead asked:** do snipe's near-synonym command clusters
(describe-symbol: `pack`/`sym`/`explain`/`show`/`def`; find-by-content:
`search`/`lits`/`trace`/`sim`; about-to-change: `plan`/`impact`/`verify`/`risk`)
cost an agent a *selection tax* — wrong-command turns and wasted tokens — or is
that an untested intuition?

**Verdict (directional):** two of the three clusters show a real selection
tax; one does not. `find-by-content` is the worst offender, `about-to-change`
second, `describe-symbol` is basically fine. A control group of unambiguous
tasks fumbled 0%, which tells us the fumbles track cluster ambiguity, not task
difficulty in general. This is a spike-grade read, not a benchmark — see
Confidence below.

---

## Finding 1 (most important): the existing eval harness cannot measure selection at all

Before any numbers, the load-bearing finding: **`test/eval/` structurally cannot
answer the bead's question**, and this is why the bead needed a new instrument.

`test/eval/eval_test.go` + `score.go` replay a **fixed command list per task**.
Each `Task` in `docs/eval/benchmark.yaml` carries a `commands:` field
(`Task.Commands` in `score.go`), and `runTask` executes exactly those commands:

```
for _, cmdStr := range task.Commands {
    args := parseCommand(cmdStr)
    stdout, stderr, exitCode := run(t, repoDir, args...)
    ...
}
```

The command is **chosen by the benchmark author and hardcoded**. The harness then
scores whether the *output* localized the right symbol/file (FileAccuracy,
SymbolAccuracy, MRR). It never observes — because it never asks — *which command
a fresh agent would reach for*. Selection is assumed away before the test starts.

Consequence: you cannot get a "before/after eval score" for a command
*restructure* out of this harness, because collapsing `lits` into `search` would
change which command an agent *picks*, and the harness has no picking step. Any
selection measurement — including the before/after a restructure proposal would
need — must come from an instrument that gives an agent the natural-language task
plus the CLI's own `--help`, and records what it tries. That instrument did not
exist; this spike built one.

## Finding 2: the live-selection instrument (reusable)

`docs/eval/selection-spike/tasks.yaml` defines 11 natural-language tasks over a
real corpus (go-chi/chi, indexed `--embed-mode=off`), grouped by the cluster
each probes, with the snipe-author's *intended* command recorded per task (for
scoring only — never shown to the solver). Method:

1. Clone + index a real Go repo the agent has never seen.
2. Hand a fresh agent ONE natural-language question and access only to
   `snipe --help` / `snipe <cmd> --help` — blind to snipe's own source, so it
   cannot read the answer or the author's intent.
3. Record: commands tried (in order), first useful command, distinct commands
   tried, whether it retried, tool-calls-to-useful.
4. Fumble = the intended command **fails, returns a misleading/incomplete
   result, or a near-synonym competes** such that a reasonable agent lands wrong
   or has to retry.

The instrument is the durable deliverable — it can be re-run before/after any
future consolidation to produce the selection before/after the harness can't.

## Finding 3: per-cluster fumble signal (directional, operator-run)

Fumble evidence below is from **operator-run command traces** against the live
corpus, not from independent blind agents — see Confidence. Each row is a
reproducible tool fact (the command's actual behavior on that framing), which is
what makes the signal hold up regardless of who ran it.

### Ranked by fumble rate (worst first)

| Rank | Cluster | Commands | Fumble rate | Nature of the tax |
|---|---|---|---|---|
| 1 | **find-by-content** | search / lits / trace / sim | **~83% (2.5/3)** | commands overlap heavily; two of four fail on a plausible framing; the "right" one is unclear or unavailable |
| 2 | **about-to-change** | plan / impact / verify / risk | **~50% (1.5/3)** | `verify` is diff-scoped but the natural framing is pre-change; `plan` needs disambiguation |
| 3 | **describe-symbol** | pack / sym / explain / def / show | **~17% (0.5/3)** | commands work; overlap is mild (`def` vs `sym`); no failures |
| — | control (baseline) | index / callers | **0% (0/2)** | unambiguous tasks don't fumble — confirms fumbles track ambiguity |

### Evidence per task

**find-by-content — rank 1, the clear problem cluster:**

- **F1** "find every place the literal `X-Forwarded-For` shows up" —
  `lits "X-Forwarded-For"` returns **1** result (the `xForwardedForHeader`
  const); `search "X-Forwarded-For"` returns **37** (code + README). The task
  wording points at `search`, the "string literal" wording points at `lits`, and
  the two give divergent answers. An agent cannot tell from the names that `lits`
  resolves *declared* literals while `search` is raw text. **Divergent-overlap
  fumble.**
- **F2** "trace how `RequestIDKey` flows through the request lifecycle" —
  `trace RequestIDKey` **errors**: `no string refs found for RequestIDKey`, and
  its own `next:` hint sends you to `search`. `trace` only follows *string
  literals* through call context; a context-key var isn't one. **Hard fumble —
  intended command fails, must fall back.**
- **F3** "where does chi protect against IP spoofing / client-IP extraction
  (concept, no name)" — `sim "..."` **errors**: `no API key ... set
  SNIPE_VOYAGE_API_KEY`. The one command built for concept→code
  (`sim`, semantic) is unavailable on an offline index, and its name advertises a
  capability the built index can't deliver. **Hard fumble.**

**about-to-change — rank 2:**

- **P3** "which tests should I run before I modify `middleware/throttle.go`" —
  `verify` returns `no Go changes — nothing to verify`: it maps a *diff* to tests
  and there's no diff yet. The pre-change framing has no clean home — `verify`
  needs a diff, `plan`/`tests` need a symbol not a file. **Hard fumble.**
  (`tests ThrottleWithOpts` does answer it — but requires the agent to already
  know the symbol and abandon the file-oriented framing.)
- **P1** "I'm about to change the signature of `URLParam`" — `plan URLParam` is
  ambiguous (2 symbols), needs `--id`. One retry then works. **Soft fumble.**
- **P2** "blast radius of changing `GetClientIP`" — `impact GetClientIP` works
  cleanly (20 results with `direct_caller`/`transitive_caller`/`direct_test`
  hints). **No fumble** — `impact` is well-named for its framing.

**describe-symbol — rank 3, basically fine:**

- **D1** `pack Mux` — clean, rich (struct + refs + role + purpose). No fumble.
- **D2** `explain Mux.ServeHTTP` — clean; bare `explain ServeHTTP` is ambiguous
  but the candidate list resolves it. No hard fumble.
- **D3** "where `URLParam` is defined and what calls it, skip packages" — `sym`
  and `def` both hit the same disambiguation and both work; the only risk is an
  agent picking `def` (definition only) when the task also wants callers (`sym`).
  **Soft distinction, not a failure.**

**control — baseline:**

- **C1** `index` and **C2** `callers Use` — single obvious command each, both
  clean. 0 fumbles. The control matters: it shows a snipe task with an obvious
  command doesn't fumble, so cluster fumble rate isn't just "agents are bad at
  snipe" — it's specific to the ambiguous clusters.

## Finding 4: two objective smells confirmed independent of agent behavior

The bead flagged two structural smells; both reproduce as plain tool facts:

- **`lifecycle` naming collision** — `lifecycle <Type>` is a top-level command
  *and* `diagram lifecycle <type>` is a subcommand (both in `cmd/cli.go` help
  output). Two different outputs share one verb.
- **`metrics` vs `hotspots` facet inconsistency** — `metrics --kind=churn|usage|...`
  is a multiplexer, but `hotspots` (complexity × churn) sits *outside* it as its
  own top-level command, though it's conceptually the same family.

---

## Restructure proposals (above threshold only — NOT implemented this spike)

Threshold: propose only for clusters above **40% fumble**. `find-by-content`
(~83%) and `about-to-change` (~50%) clear it; `describe-symbol` (~17%) and the
control do not, so they get no restructure proposal — the intuition that
`pack`/`sym`/`explain` need collapsing is **not supported** by this read.

Per SDLC spike rules and the coordinator's explicit call, **this spike ships the
finding + instrument, not a restructure.** The before/after selection scores a
consolidation needs must come from re-running `tasks.yaml`'s instrument (Finding
1: the old harness is blind to selection) — that is follow-up work, filed as a
proposal, gated on a decision:

1. **find-by-content — make the failure modes legible; consider folding `lits`.**
   The tax here isn't too many commands, it's that `trace` and `sim` *silently
   promise* capability the input/index can't deliver. Cheapest high-value fix:
   `trace` (no string refs) and `sim` (no embeddings) already redirect to
   `search` on failure — make `search` the documented front door ("start with
   `search`; `lits` for declared literals, `trace` for literals through call
   flow, `sim` for concept search *when embeddings exist*") and surface those
   preconditions in each command's one-line help. Consider folding `lits` into
   `search --literals`, since F1 shows they answer the same user question with
   divergent completeness.

2. **about-to-change — `verify` needs a pre-change entry, or a redirect.** P3 is
   the sharp edge: "which tests before I touch file X" has no home. Proposal:
   accept a symbol/file target on `verify` (map file → symbols → tests) so the
   pre-change framing resolves, or emit a `next: snipe tests <symbol>` hint when
   `verify` finds no diff. `plan`'s ambiguity (P1) is a general disambiguation
   nit, not a cluster problem.

3. **naming smells (Finding 4) — cheap, no measurement needed.** Rename the
   `diagram lifecycle` subcommand or the top-level `lifecycle` so one verb ≠ two
   outputs; fold `hotspots` under `metrics --kind=hotspots` for facet
   consistency. These are legibility fixes, not selection-tax fixes; they don't
   need the before/after gate.

---

## Confidence / limitations (read before citing these numbers)

- **Sample: operator-run, not blind-agent.** 11 blind subagents were dispatched
  to run `tasks.yaml` independently, but they contended for fleet resources and
  were stopped before returning results (0 of 11 landed). The fumble evidence
  above is instead **operator-run command traces** — the candidate commands were
  run against the live corpus and scored on the tool's actual behavior. That is
  weaker on *which command an agent would instinctively pick* (the operator knows
  the tool), but the strongest findings — `trace` errors, `sim` needs embeddings,
  `verify` needs a diff, `lits`≠`search` — are reproducible tool facts
  independent of who runs them.
- **N is directional.** 11 tasks, 1 corpus, 1 offline index. Enough for a
  go/no-go read on which clusters to investigate; **not** a statistical fumble
  rate. Treat the percentages as ordering, not precision.
- **Offline-index artifact.** F3 (`sim`) and part of F2 fail partly because the
  index has no embeddings. That is itself a selection signal (the command name
  over-promises), but a fully-embedded index would move F3 from "fails" to
  "works but competes with `search`."
- **To harden:** re-run `tasks.yaml` with independent blind agents (the intended
  design), on ≥2 corpora, one with embeddings, and record real
  tokens-to-first-useful from agent transcripts rather than operator estimates.
  That produces the true before-number for any restructure.
