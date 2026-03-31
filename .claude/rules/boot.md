# Boot

sha: a4b5cbb | updated: 2026-03-31

## Do next

Pick one: #127 un-skip impl test | #117 semsearch unit tests | #128 v0.1.1 tag

## Done

- Switched from mage to make — Makefile modeled after trixi's, `make` (check) + `make ci` (full) + `make deploy`
- Pre-existing gocritic lint issue in cmd/search.go:152 (ifElseChain) — not blocking check, will surface in `make ci`

## Backlog

#127 un-skip impl test | #112 vague-intent search | #117 semsearch unit tests | #128 v0.1.1 tag | #129 Homebrew tap | #122 human/table format | #126 trixi audit

## Traps

- Credentials file uses SNIPE_VOYAGE_API_KEY (renamed 2026-03-18) — old key name will silently not work
- Orca fixed — passes `--format json` now. If snipe's JSON envelope changes, re-verify orca.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- `make check` uses staticcheck only; `make ci` runs full golangci-lint (gocritic etc.)
