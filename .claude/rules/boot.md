# Boot
updated: 2026-05-07

→ next: snipe-4ip — pick one of the 6 real recall gaps. Lowest-hanging: orca-search-08 (single-symbol index miss) or cobra-refs-01 (refs ordering/limit). Hardest: orca-search-04 (structural — search results lack enclosing-symbol metadata).

✓ done (this session)
- snipe-0x5, snipe-2in: 5 callees-spec bugs (self-referential / transitive expectations)
- 6 more spec bugs in one pass (bbolt-callers-01, orca-pack-01, fzf-search-02, orca-cross-01, orca-pkg-03, orca-cross-02)
- Eval: 76% → 92% scored pass (57/72 → 66/72)

‡ traps
- New `lifecycle.Role` value → update exhaustive switch in `cmd/diagram.go:edgeFromType`
- New BootContext field that adds a `## section` → regen golden via `UPDATE_GOLDENS=1 go test -tags blackbox ./test/blackbox/ -run TestGolden_Context`
- Eval benchmark.yaml: callees expects *direct* (1-hop) callees, callers expects *direct* (1-hop) callers. Don't list transitive.
