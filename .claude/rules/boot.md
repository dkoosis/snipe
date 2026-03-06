sha: 6d977e8 | qa: pass | updated: 2026-03-05

## Do next

Brainstorm #110 change impact (`snipe impact <symbol>`), then implement.

1. `mage` — confirm green baseline
2. Invoke brainstorming skill for #110 — user approved starting this
3. Design: "if I change this, what breaks?" — callers (transitive), implementors, tests (#108 data)
4. Tag v0.1.1 still pending — do after #110 or when user asks

## Done

- #108 test mapping shipped: `snipe tests <symbol>` with 2-hop transitive call graph, `--direct`, `--at`, zero-coverage suggestions
- Enabled test file indexing (`Tests: true` in go/packages config, `OR IGNORE` for dedup)
- #107 convention detection shipped: 6 detectors, `--conventions` flag, boot context integration
- Merged PR #114 (AST edit tests from Codex), fixed lint issues
- Simplify pass: fixed 5 missing `rows.Err()` checks, eliminated N+1 in detectTesting

## Backlog

#110 change impact | #112 token budget | #113 deps type consolidation | v0.1.1 tag | orca persistToolCall | Homebrew tap

## Traps

- v0.1.0 tag at 834d19e — v0.1.1 still needed (has linux/arm64 + Stream E + deps + tests + conventions)
- `generatePurpose()` runs every index writing placeholder strings — first thing to stop
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands (tests was missing)
- `Tests: true` in indexer causes 3x package variants — `INSERT OR IGNORE` handles dedup
- Do NOT optimize eval until telemetry provides ground truth
