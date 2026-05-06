# Boot
updated: 2026-05-06

→ next: snipe-84h (interface-method classification — needs method-call-site analysis: iterate interface methods, find their refs, classify enclosing fns by method verb). Then P3 backlog: 277 boundary cross-ref, s5p context dep DAG, 9bi enrich purpose, ak9 file-header capture.

✓ done (this session, 2026-05-06)
- snipe-dhq: lifecycle accepts 16-char hex symbol ID (parity with def/pack/tests)
- snipe-29j: caller chains capped at 8 non-test names, tests folded into "+N tests" suffix
- snipe-13u: --format summary now produces per-group one-liner (was identical to default)
- snipe-0sb: standard CRUD buckets always render, even with (0)
- snipe-45t: bucket partition documented; verified no double-bucketing (visual confusion was caller-chain Test* names, fixed by 29j)
- snipe-ct7: file-scope refs split into RoleTypeUse bucket; Unknown reserved for true classification gaps
- snipe-36f: lifecycle gained --pkg and --at scope flags (parity with def/refs/pack)
- snipe-8xk: `snipe context` surfaces high-D packages as `## arch warnings` (D > 0.70, top 5)

✓ done (2026-05-05)
- snipe-fac: cyclo rollups per package (`snipe metrics --kind=cyclo`); sum/p95/max, ! flags p95 > 10 OR max > 20. cmd hottest at sum=3130, max=67.
- snipe-gq4: LCOM4 cohesion (`snipe metrics --kind=lcom4`); top split candidates cmd=41, query=33, output=21
- snipe-g63: distance-from-main-sequence (`snipe metrics --kind=distance`); D = |A+I−1|, ! flags D > 0.70

## traps (added this session)
- New `lifecycle.Role` value → add to exhaustive switch in `cmd/diagram.go:edgeFromType`
- New BootContext field used in `## section` → update blackbox golden via `UPDATE_GOLDENS=1 go test -tags blackbox ./test/blackbox/ -run TestGolden_Context`
