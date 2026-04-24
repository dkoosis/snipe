# Boot
updated: 2026-04-24

→ Implement CRUD heuristics for `snipe lifecycle` (snipe-ahi.2)
  `bd show snipe-ahi.2` → claim → classify callers into Create/Mutate/Read/Delete

state: φ commit cb24f83 | `make check` green

✓ done
- snipe-ahi.1 shipped: lifecycle scaffold, cobra wiring, knownSubcommands registered

‡ traps
- BASELINE_ORCA.json timestamp drifts on every run; ✗ stage it
- Incremental writeRefs INSERT OR IGNORE — ref.ID collision silently skips update
