# ctx-value — repo review (snipe)

run-id: ecebe5258308
date: 2026-05-17
linter: ctx-value (project scope, report mode)
scope: first-party Go (vendor/ excluded)

## Summary

🟢 Clean. Zero `context.WithValue` and zero `ctx.Value(...)` call sites in first-party code. All matches live under `vendor/` (stretchr/testify, golang.org/x/tools, google/uuid) and are not authored here.

## Evidence

```
$ rg -n 'context\.WithValue|ctx\.Value\(' --type go -g '!vendor/**' -g '!.snipe/**'
(no matches; exit 1)
```

- P1 hidden deps: 0 → 🟢
- P1 fn-takes-ctx-reads-biz: 0 → 🟢
- P2 type-assert explosion: 0 → 🟢 (no consumers exist)
- P2 untyped/string keys: 0 → 🟢
- P3 stacked WithValue chain: 0 → 🟢

## Findings

None. Cap 10; 0 used.

## Notes

snipe propagates `context.Context` through commands and store/index layers as a deadline/cancellation carrier only — no key/value smuggling. This is the desired posture for the linter. No action.

If future work adds request-scoped data (e.g., a trace ID from orca telemetry, or a per-call logger), introduce it via an unexported key type + typed accessor in a single package before the first `WithValue` call site lands.
