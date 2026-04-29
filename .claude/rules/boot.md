# Boot
updated: 2026-04-29

state: main @ 01523ed, `make audit` green

→ next: `bd ready` and pick

✓ done
- pack: package-mode tests + flaky golden fix (snipe-tl2 / #126 M4)
- embed: threaded ctx through 5 Voyage HTTP sites + cancellation test (snipe-8ul)

‡ traps
- new cobra subcommand → register in knownSubcommands (cmd/root.go) or routes to sym fallback
- httptest blocking handlers must observe a stop chan, not just r.Context().Done() — server.Close() WaitGroups on the handler
- key_symbols ranking: same-name symbols across pkgs (e.g. alpha.X vs beta.X) need file-path tiebreaker — name-only sort is non-deterministic
