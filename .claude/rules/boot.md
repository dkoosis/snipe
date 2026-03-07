sha: c8c69e4 | qa: pass | updated: 2026-03-06

## Do next

Pick one: un-skip impl test | #121 search index fallback | #123 root detection

## Done

- Impact query layer: FindImpactCallers/Multi, FindMethodIDs, FindTests/Multi with tests
- Orca updated to pass `--format json` to snipe
- Claude-optimized text output as default (~68% token reduction vs JSON)
- Fuzzy method name resolution: `callers ListFiles` finds methods without receiver syntax (#120)
- Decision table D1-D6 in CLAUDE.md + guard-rails.md (trixi-style)
- Created issues: #120 fuzzy names, #121 search fallback, #122 human format, #123 root detection

## Backlog

un-skip impl test | #121 search index fallback | #123 root detection | #112 vague-intent search | #117 semsearch unit tests | v0.1.1 tag | Homebrew tap

## Traps

- Orca fixed — passes `--format json` now. If snipe's JSON envelope changes, re-verify orca.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- Do NOT optimize eval until telemetry provides ground truth
- `generatePurpose()` runs every index writing placeholder strings
