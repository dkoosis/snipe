# Boot
updated: 2026-05-06

→ next: bbolt-search-02 / bbolt-cross-01 — last 3 known eval gaps. Likely indexer side: `freelist.allocate`, `node.put`, `node.spill`, `Tx.Commit` not surfacing. Reproduce against `../bbolt`.

✓ done
- snipe-3js: callgraph interface-dispatch cross-pkg fix. Replaced caller-imports-impl-pkg heuristic with `types.Implements`. Eval all-PASS, callers 100%, +regression test.
- snipe-yue: search augmentation w/ name+stem substring lookup. Symbol acc 95.9%→100%.
