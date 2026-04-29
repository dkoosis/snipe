# Boot
updated: 2026-04-29

state: main @ 593a8bd, `make` green (2 pre-existing blackbox golden failures unrelated)

no queued task — pick from `bd ready`

✓ done (this session)
- types: fixed embed detection — named fields like `Definition *Result` were misclassified as embedded types; now uses snippet heuristic (single-token = embed)

✓ done (prev)
- def: `--pkg <pkg> Name` now scopes lookup to package (was 50-symbol fuzzy dump) — snipe-d40
- pkg: cap standalone functions at 15, "...N more: names" footer
- context: key symbols ranked by cross-file spread, not raw ref count
- pack/types: unexported methods excluded from method lists
- explain: deduplicate mechanism steps by callee name
- pkg: methods grouped under receiver type in text output

‡ traps
- new cobra subcommand → register in knownSubcommands (cmd/root.go) or routes to sym fallback
- 2 pre-existing blackbox golden test failures (TestGolden_Pkg, TestGolden_Context_Boot) — not caused by recent changes
