sha: 794cabb
updated: 2026-02-07T04:15:00Z
qa: pass
intent: make snipe quality measurable from real usage, not synthetic benchmarks

ready: fix Orca telemetry — persistToolCall is a no-op
  - bug: `persistToolCall` at orca/internal/obs/telemetry/metrics/tool_telemetry.go:236 is empty
  - plumbing exists: LogToolCallAsync → recordToolTelemetry → sqlite_writer INSERT
  - the full ToolCallRecord is built at mcp_hooks.go:540-558 with tool, params, tokens, duration
  - fix: wire persistToolCall to call GlobalSQLiteWriter().LogToolCall()
  - verify: run orca, call go_symbol, query `SELECT * FROM orca_tool_calls`
  - then: instrument quality signals (retry detection, rg fallback, suggestion follow-through)
  - these are behavioral signals — no Claude self-assessment needed

done:
- search: auto-add --glob '*.go' for identifier queries @cmd/search.go:62
- FindSymbolAtPosition: range containment instead of exact line match @internal/query/lookup.go:1062
- engine changes produced 0 eval flips (benchmark can't measure their impact)
- attempted 5 YAML "fixes" that inflated score 80.8% → 87.5% — all reverted
- eval score unchanged at 80.8% (honest number, engine-only)

prior-session:
- pkg sort, search enrichment, interface callers, callgraph dispatch — 55.9% → 80.8%
- eval harness expanded to 76 tasks, scoring fixes, known_gap support
