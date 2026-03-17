sha: e57fc70 | qa: pass | updated: 2026-03-16

## Do next

Pick one: un-skip impl test | #121 search index fallback | #123 root detection

## Done

- Full /sweep simplify: 15/17 packages simplified, -600+ lines dead code/duplication
- Full /sweep craft: 14/17 packages improved, found+fixed 6 bugs (token estimate accumulation, defer-in-loop false positive, search ID ambiguity, config silent fallback, impl panic on empty args, magefile qa fallback)
- Error wrapping added across store, metrics, index, query, config
- Deterministic output ordering in output package
- test/bench deduplicated 350 lines of shadowed types

## Backlog

un-skip impl test | #121 search index fallback | #123 root detection | #112 vague-intent search | #117 semsearch unit tests | v0.1.1 tag | Homebrew tap

## Traps

- Orca fixed — passes `--format json` now. If snipe's JSON envelope changes, re-verify orca.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- Do NOT optimize eval until telemetry provides ground truth
