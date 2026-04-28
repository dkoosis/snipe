# Boot
updated: 2026-04-28

→ `bd ready` — only snipe-bvf open (embed research, blocked on telemetry). Open new work or declare done.

state: main @ 0a5c431, `make audit` green

✓ done
- trace cmd: string literal → enclosing func + callers (∈ F ← G, H format)
- pack <pkg>: key_types/key_funcs ranked by ref/call count; export dump removed
- output.Result.Doc: first sentence surfaced in all commands (def, pkg, refs, callers…); struct/type match line suppressed (was redundant qualified name)

‡ traps
- new cobra subcommand → register in knownSubcommands (cmd/root.go) or routes to sym fallback
