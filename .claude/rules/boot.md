sha: 0504d1c | qa: pass
updated: 2026-02-26

## Do next

Tag v0.1.1 to pick up linux/arm64 and error message fixes missed in v0.1.0.

1. Verify v0.1.0 release built successfully: `gh run list --limit 3`
2. `git tag v0.1.1 && git push origin v0.1.1`
3. Close #88 with link to release

## Done

- #88 M4 distribution shipped: goreleaser + GitHub Actions + v0.1.0 tagged (848cdd7)
- README rewritten: accurate output examples, correct Go 1.24+ requirement, professional voice
- Fixed post-tag: added linux/arm64, replaced em dash in error message (0504d1c)
- Snapshot build validated locally: 4 binaries, ldflags stamp version+commit
- Simplified 6 files (-127 lines), merged PR #104 audit, closed #102

## Backlog

#96 batch embed RAM | orca persistToolCall | Windows builds | Homebrew tap | `debug.ReadBuildInfo` for go install

## Traps

- v0.1.0 tag is at 834d19e (before linux/arm64 fix) — v0.1.1 needed for clean release
- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- Do NOT optimize eval until telemetry provides ground truth
- Audit findings at `docs/audits/2026-02-26-true-bug-audit.md` — FK + error suppression are high-severity
