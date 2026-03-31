# Boot

sha: 8ead52d | updated: 2026-03-31

## Do next

Pick one: #127 un-skip impl test | #117 semsearch unit tests | #128 v0.1.1 tag

## Done

- Context output quality: 9 fixes landed (820→624 lines), command table compression, flow depth, purpose strings, build target filter, README fallback
- `make ci` passes clean — goconst fixes included
- Plan: `docs/superpowers/plans/2026-03-31-context-output-quality.md`

## Backlog

#127 un-skip impl test | #112 vague-intent search | #117 semsearch unit tests | #128 v0.1.1 tag | #129 Homebrew tap | #122 human/table format | #126 trixi audit

## Traps

- Credentials file uses SNIPE_VOYAGE_API_KEY (renamed 2026-03-18) — old key name will silently not work
- Orca fixed — passes `--format json` now. If snipe's JSON envelope changes, re-verify orca.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- `make check` uses staticcheck only; `make ci` runs full golangci-lint (gocritic etc.)
- `BootViews.EntryPointDetails` replaced by `BootViews.Commands` — any downstream consumers of the JSON field name need updating
