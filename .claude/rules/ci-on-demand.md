# CI On-Demand — opt expensive CI in at PR time

*Codex review (OpenAI spend) no longer runs on every PR. Claude opts a PR in via a comment when the diff warrants it. Default OFF; unsure → don't opt in.*

## Codex review — comment `@codex review`

Advisory; does **not** gate merge (`make audit` / `check` is the gate).

```bash
gh pr comment <PR#> --body "@codex review"
```

| Request when diff has | Skip for |
|---|---|
| new/changed logic w/ branching or edge cases | docs / config / test-only |
| concurrency / lifecycle / goroutine code | mechanical renames/moves, `s/this/that/g` sweeps |
| persistence / write paths | dependency bumps |
| scoring / retrieval / output-format changes | one-liners |
| security-adjacent (auth, SSRF, input handling) | generated code |
| large / sprawling change | reverts / cherry-picks of already-reviewed work |
