# Boot
updated: 2026-04-24

→ Text + JSON output formatters (snipe-ahi.4)
  `bd show snipe-ahi.4` — render lifecycle groups as Claude-readable text + JSON

state: `make check` green

✓ done
- snipe-ahi.1: lifecycle scaffold, cobra wiring
- snipe-ahi.2: CRUD classifier (R1-R8 rule table, 90% cov, dogfooded on SymbolRow/Writer/Store)
- snipe-ahi.3: caller-chain BFS (WalkCallers, --depth flag, cycle detection, 7 tests)

‡ traps
- Indexer skips signature-line refs (enclosing_id null on `func F() *T`). Worked around in cmd/lifecycle.go via reattachSignatureRefs
- R2 snippet regex must guard against `func F() *T {` matching as struct literal — see isFuncDeclLine
- BASELINE_ORCA.json timestamp drifts on every run; ✗ stage it
