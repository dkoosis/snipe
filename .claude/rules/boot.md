# Boot
updated: 2026-05-07

→ next: pick from snipe-4ip remaining gaps via `bd show snipe-4ip`. 4 left: orca-callers-01 (cross-pkg call graph), cobra-cross-01, fzf-cross-01, orca-search-04 (structural — search needs enclosing-symbol). Plus surfaced cobra-search-01 (GenZshCompletion recall miss).

✓ done
- orca-search-08: stale-spec fix (GoSymbolService refactored in orca)
- cobra-refs-01: real fix — FindRefs ORDER BY now biases def-file first

‡ traps
- Eval uses `../cobra` (full repo w/ _test.go), not `.eval-repos/cobra`. Reproduce eval results from `/Users/vcto/Projects/cobra`.
