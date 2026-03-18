sha: 79c1851 | qa: pass | updated: 2026-03-18

## Do next

Pick one: un-skip impl test | #121 search index fallback | #123 root detection

## Done

- Renamed VOYAGE_API_KEY → SNIPE_VOYAGE_API_KEY throughout (env var, credentials file, docs, tests)
- Full /sweep simplify: 15/17 packages simplified, -600+ lines dead code/duplication
- Full /sweep craft: 14/17 packages improved, found+fixed 6 bugs

## Backlog

un-skip impl test | #121 search index fallback | #123 root detection | #112 vague-intent search | #117 semsearch unit tests | v0.1.1 tag | Homebrew tap

## Traps

- Credentials file uses SNIPE_VOYAGE_API_KEY (renamed 2026-03-18) — old key name will silently not work
- Orca fixed — passes `--format json` now. If snipe's JSON envelope changes, re-verify orca.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- Do NOT optimize eval until telemetry provides ground truth
