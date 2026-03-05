sha: c34271e | qa: pass
updated: 2026-03-05

## Do next

Tag v0.1.1 to pick up linux/arm64 fix + Stream E features, then push.

1. `gh run list --limit 3` — verify v0.1.0 release built
2. `git tag v0.1.1 && git push origin v0.1.1`
3. Close #88 with link to release

## Done

- Stream E quick wins shipped: package narratives (#105), interface map (#109), build detection (#111)
- Simplify review: deduplicated interface method extraction, consolidated build detection, upsert for package_docs (-105 lines)
- 8 GitHub issues created (#105-#112) for Claude orientation improvements
- `docs/progress.md` updated with Stream E section

## Backlog

#106 dependency topology | #107 convention detection | #108 test mapping | #110 change impact | #112 token budget | #96 batch embed RAM | orca persistToolCall | Homebrew tap

## Traps

- v0.1.0 tag is at 834d19e (before linux/arm64 + Stream E) — v0.1.1 needed
- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Audit findings at `docs/audits/2026-02-26-true-bug-audit.md` — FK + error suppression are high-severity
- Worktrees at `.claude/worktrees/agent-*` — clean up when convenient
