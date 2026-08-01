# CI On-Demand — bot review is judge-gated

*Advisory bot review (Codex/Gemini) fires automatically at PR open, scored by a deterministic judge — not opted in by hand anymore.*

## Bot review — automatic, judge-gated

`.github/workflows/bot-review.yml` is a thin caller stub; the pipeline lives once in `dkoosis/cc-plugins` (`bot-review.yml` reusable workflow + `actions/review-judge`). Fires on PR open / draft→ready.

The judge scores via `snipe risk <base> <head>` — this repo IS snipe, so it scores its own PRs with itself. That needs a vendored `linux-amd64` snipe binary at `.sandbox/bin/linux-amd64/snipe`; `.github/workflows/refresh-vendored-snipe.yml` rebuilds it on every push to `main` so the judge never scores against a stale binary (found 3+ months stale before this was wired — a manual `make cross` had drifted).

| Tier | Fires | Signal |
|---|---|---|
| `none` | nothing | `snipe risk` verdict low, small diff, no churn hotspot |
| `codex` | Codex | verdict medium, or ≥80 added Go lines, or a churn-hotspot file touched |
| `full` | Codex + Gemini | verdict high, or ≥300–800+ added lines, or multiple signals stack |

When `snipe`/`jq` are unavailable or the diff is degraded (non-Go, cold index), the judge falls back to a portable token scan (concurrency/security keyword hits) — never trusts an unanalyzable diff as clean.

**✗ opt PRs in by hand at creation** — the judge does it. Manual paths are for exceptions only:

```bash
gh pr comment <PR#> --body "@codex review"    # force Codex (re-review after a big push, drafts)
gh pr comment <PR#> --body "@gemini review"   # force Gemini
```

Advisory only; nothing here gates merge (`make audit` / `check` is the gate). Tier + reasons land in the run's step summary. `synchronize` deliberately doesn't re-judge — force a re-review by comment after a substantial post-open push.

Secrets: `OPENAI_API_KEY` (Codex, required); `GEMINI_API_KEY` (Gemini, optional — job skips cleanly while unset).
