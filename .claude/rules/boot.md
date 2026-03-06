sha: fdb50e0 | qa: pass | updated: 2026-03-06

## Do next

Un-skip `TestImpl_ReturnsImplementations_ForInterface` in `test/blackbox/cli_workflows_test.go:223` — impl fix is verified by sweep tests now.

1. `mage` — confirm green
2. Un-skip the impl test, verify pass
3. Tag v0.1.1 when user approves

## Done

- Updated Codex sandbox: golangci-lint v2.8.0 (Go 1.25.5), CLI=1 for fo bypass
- Applied same sandbox fixes to ../trixi (golangci-lint + fo stub)

## Backlog

un-skip impl test | #112 vague-intent search | #117 semsearch unit tests | v0.1.1 tag | orca persistToolCall | Homebrew tap

## Traps

- v0.1.0 tag at 834d19e — v0.1.1 still needed
- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- Do NOT optimize eval until telemetry provides ground truth
