sha: 75712ff | qa: pass
updated: 2026-03-05

## Do next

Tag v0.1.1 to pick up linux/arm64 fix + Stream E features + deps command + sandbox, then push.

1. `gh run list --limit 3` — verify v0.1.0 release built
2. `git push origin main`
3. `git tag v0.1.1 && git push origin v0.1.1`
4. Close #88 with link to release, close #106 with commit ref

## Done

- `snipe deps` command shipped (#106): single-package + full-graph modes, cycle detection, blackbox tests
- Sandbox replicated from trixi: `.codex/` scripts, `.bin/linux-{amd64,arm64}/` prebuilts (11 tools each), `mage cross`, `settings.local.json`, AGENTS.md
- Self-review: fixed fragile path heuristic (DB lookup), cycle detection parent-map bug (stack-based), filed #113

## Backlog

#107 convention detection | #108 test mapping | #110 change impact | #112 token budget | #113 deps type consolidation | orca persistToolCall | Homebrew tap

## Traps

- v0.1.0 tag is at 834d19e (before linux/arm64 + Stream E + deps) — v0.1.1 needed
- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- `.claude/settings.local.json` is in global gitignore — won't be committed, stays machine-local
- Worktrees at `.claude/worktrees/agent-*` — clean up when convenient
