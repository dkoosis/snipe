# CI On-Demand — bot review is app-based, not pipeline-based

*Superseded 2026-08-02: the judge-gated `cc-plugins` pipeline never worked on this repo — GitHub forbids a public repository from calling a reusable workflow in a private one, and cc-plugins is private. Every run failed instantly with zero jobs from day one (#215). Removed `bot-review.yml` + `refresh-vendored-snipe.yml`; that pipeline still works fine on dk's other (private) repos, e.g. `trixi`, `canapay` — this is a snipe-specific constraint, not a pipeline bug.*

## Bot review — GitHub Apps, no repo workflow

No CI file drives this. Two GitHub Apps review PRs directly, account-connected:

| App | Behavior |
|---|---|
| Codex | Auto-reviews on PR open (per its own dashboard config). Force: `gh pr comment <PR#> --body "@codex review"` |
| CodeRabbit | Auto-reviews on PR open. Force: `gh pr comment <PR#> --body "@coderabbitai review"`. Has a per-plan rate limit — a rate-limited review needs a retry after the cooldown it reports |

Gemini Code Assist (consumer) is sunset — do not rely on it. Cursor Bugbot is disabled on this account.

Advisory only; nothing here gates merge (`make audit` / `check` is the gate).
