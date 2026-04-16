# Boot
updated: 2026-04-16

→ Pick one from #126 remainder (M1/M3/M4/M5) or tackle #135 lifecycle.
   Cheap M5 first: fold `deps` internal DAG into `context --boot` output.

state: φ commit 5a56912 | `make check` green

✓ done
- Audit waves A/B/C shipped: F1/F2/F3/F4/F5/F6/F7/F8/F9/F10 + M2/M6
- Plan review caught that F6 was mostly built (just routing bug), saved rework
- #126 left open with progress comment; M1/M3/M4/M5 still open there

‡ traps
- SNIPE_VOYAGE_API_KEY is the current name (not VOYAGE_API_KEY)
- Orca consumes `--format json`; re-verify if envelope changes
- `knownSubcommands` in cmd/root.go must be updated for any new command
- No production interfaces — impl testable only via blackbox fixture
- BASELINE_ORCA.json timestamp drifts on every run; ✗ stage it
- Incremental writeRefs uses INSERT OR IGNORE — if ref.ID collides, row is not refreshed
