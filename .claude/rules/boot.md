# Boot
updated: 2026-04-29

→ snipe-d40 (P2): `def --pkg <pkg> MethodName` falls through to 50-symbol fuzzy dump instead of candidates

state: main @ 86e1466, `make audit` green

✓ done (this session)
- pkg: cap standalone functions at 15, "...N more: names" footer (was 400+ line dump)
- context: key symbols ranked by cross-file spread, not raw ref count (fixes fzf over-indexing)
- pack/types: GetMethodsForType now exports-only (drops unexported methods from method lists)
- explain: deduplicate mechanism steps by callee name (was showing "calls lookupMethod" twice)

✓ done (prev)
- pkg: methods grouped under receiver type in text output

‡ traps
- new cobra subcommand → register in knownSubcommands (cmd/root.go) or routes to sym fallback
- def --pkg <pkg> MethodName: fuzzy fallback dumps all pkg symbols (snipe-d40)
