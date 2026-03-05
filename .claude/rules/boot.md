sha: d628604 | qa: pass
updated: 2026-03-05

## Do next

Tag v0.1.1 — main is pushed, #106 and #88 closed. Just tag and release.

1. `gh run list --limit 3` — verify v0.1.0 release built
2. `git tag v0.1.1 && git push origin v0.1.1`
3. Verify release artifacts appear on GitHub

## Done

- Cleaned repo: 3 worktrees removed, 3 orphan branches deleted, stale stash dropped, #106 closed
- Simplified deps command: extracted scanDepEdges helper, removed dead PackageFull field, extracted depsMeta
- Main pushed to remote, synced

## Backlog

#107 convention detection | #108 test mapping | #110 change impact | #112 token budget | #113 deps type consolidation | orca persistToolCall | Homebrew tap

## Traps

- v0.1.0 tag is at 834d19e (before linux/arm64 + Stream E + deps) — v0.1.1 needed
- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- `query` must NOT import `output` — layering violation caught and reverted this session
