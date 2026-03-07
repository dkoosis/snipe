sha: c100f73 | qa: pass | updated: 2026-03-06

## Do next

Orca integration: `go_symbol` calls snipe with no `--format` flag, so it now gets Claude text instead of JSON. Either update orca to pass `--format json` or teach it to parse the new format.

1. Check `../orca/internal/mcp/server/go_symbol_handler.go` for snipe subprocess calls
2. Add `--format json` to all snipe invocations in orca
3. Run orca's go_symbol tests to verify

## Done

- Claude-optimized text output as default (~68% token reduction vs JSON)
- Fuzzy method name resolution: `callers ListFiles` finds methods without receiver syntax (#120)
- Decision table D1-D6 in CLAUDE.md + guard-rails.md (trixi-style)
- Created issues: #120 fuzzy names, #121 search fallback, #122 human format, #123 root detection

## Backlog

orca --format json fix | un-skip impl test | #121 search index fallback | #112 vague-intent search | #117 semsearch unit tests | v0.1.1 tag | Homebrew tap

## Traps

- Orca will break on next snipe update — expects JSON, now gets text. Fix orca first.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- Do NOT optimize eval until telemetry provides ground truth
- `generatePurpose()` runs every index writing placeholder strings
