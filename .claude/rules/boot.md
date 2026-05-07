# Boot
updated: 2026-05-06

→ next: bbolt-cross-01 (Tx.Commit). Only reachable transitively from `callers node.spill` (Tx.Commit → Bucket.spill → node.spill). Either teach `callers` a `--depth` / transitive flag, or rewrite the benchmark task with a 4th command. Also: bbolt-search-01 still listed as known_gap — verify post-fix and prune if clean.

✓ done
- snipe-lwk: lookup.go method-lookup now triggers for unexported T.Method (`node.put`, `freelist.Free`). Closed bbolt-search-02; bbolt-cross-01 reduced 3→1 missing.
- snipe-3js: callgraph interface-dispatch cross-pkg fix. Replaced caller-imports-impl-pkg heuristic with `types.Implements`. Eval all-PASS, callers 100%, +regression test.
- snipe-yue: search augmentation w/ name+stem substring lookup. Symbol acc 95.9%→100%.
