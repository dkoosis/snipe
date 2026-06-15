# Boot
updated: 2026-06-15 (76d9975)

→ next: `bd ready` — testscript suite (snipe-p61) still `human`-flagged; wait for dk. Watch `snipe metrics --kind=usage` accumulate; re-mine transcripts ~mid-July against the 4:1 rg baseline. ‡ eval siblings need reindex before score comparisons (.test pkg removal changed symbol counts).

✓ done
- `snipe guard` shipped (#162, snipe-guard): reference-graph boundary gate, kind filter, ratchet allow-list, fail-closed exits. Live-caught 6 agent→store violations in trixi. Post-review fix: allow-list suffix now path-aligned (`db.go` ≠ `validb.go`).
- index-time invariants pass (5 beads, one session): "fix at write, not read"
  - .test binary pkgs dropped post-load — go-build cache pollution gone (111→0 symbols)
  - subcommand set derived from cobra tree (sync.Once); knownSubcommands map deleted
  - deterministic ORDER BY ×15 + source-scanning guard test in internal/store
  - enclosing_id on signature refs at write (fn.Pos() range); reattachSignatureRefs deleted; orphans 1019→355
  - refs.ast_ctx column (schema v18): lit/new/make/sig/typedecl/call:<name>; lifecycle R1/R2/R6 read stored facts, snippet create-regexes deleted
- prior: snipe-g0d epic (8/8) first-reach excellence pass → docs/progress.md
