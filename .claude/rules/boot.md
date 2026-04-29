# Boot
updated: 2026-04-29

state: main @ 34d30d7, `make audit` green

no queued task — pick from `bd ready`

✓ done (this session)
- def: `--pkg <pkg> Name` now scopes lookup to package (was 50-symbol fuzzy dump) — snipe-d40

✓ done (prev)
- pkg: cap standalone functions at 15, "...N more: names" footer
- context: key symbols ranked by cross-file spread, not raw ref count
- pack/types: unexported methods excluded from method lists
- explain: deduplicate mechanism steps by callee name
- pkg: methods grouped under receiver type in text output

‡ traps
- new cobra subcommand → register in knownSubcommands (cmd/root.go) or routes to sym fallback
