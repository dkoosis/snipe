# Boot

sha: ad42a17 | updated: 2026-03-31

## Do next

Pick one: #127 un-skip impl test | #117 semsearch unit tests | #128 v0.1.1 tag

## Done

- Context output quality round 2: command purposes from Cobra Short (34/37), flow diversity via priority+dedup, callee ranking by significance
- Removed build/test duplication, suppressed active_work on main, fixed root package purpose, rounded float noise
- Output: 624→623 lines, `make ci` clean

## Backlog

#127 un-skip impl test | #112 vague-intent search | #117 semsearch unit tests | #128 v0.1.1 tag | #129 Homebrew tap | #122 human/table format | #126 trixi audit

## Traps

- Credentials file uses SNIPE_VOYAGE_API_KEY (renamed 2026-03-18) — old key name will silently not work
- Orca passes `--format json`. If snipe's JSON envelope changes, re-verify orca.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- `BootContext.Build`/`.Test` removed — use `BuildInfo.Build`/`.Test` now. Downstream consumers of top-level JSON `build`/`test` fields need updating.
