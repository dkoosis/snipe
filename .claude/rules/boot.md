# Boot
updated: 2026-06-12

→ next: `bd ready` — testscript suite (snipe-p61) still `human`-flagged; wait for dk. Watch `snipe metrics --kind=usage` accumulate; re-mine transcripts ~mid-July against the 4:1 rg baseline.

✓ done
- snipe-g0d epic (8/8): first-reach excellence pass
  - fixed module-path detection (go.mod authoritative) + context --full/--conventions default format
  - every error routes forward (Next by code in WriteError)
  - self-healing index (≤20 files drift reindexes inline, SNIPE_NO_HEAL opt-out)
  - qualified names round-trip (pkg.Type.Method, (*T).Method display = query form)
  - usage telemetry → .snipe/usage.jsonl + metrics --kind=usage
  - cc-plugins/snipe: SessionStart context inject + Grep nudge hooks
  - snipe-first lines in trixi/loto/fo/snipe rules

‡ new trap: `snipe search` can surface go-build cache paths (../../Library/Caches/go-build/...) — semantic index pollution, unfiled
