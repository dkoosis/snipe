# Boot
updated: 2026-05-06

→ next: 3 remaining snipe-4ip failures + 1 partial. orca-callers-01 (file-miss, cross-pkg call graph), cobra-cross-01 (search 'Args' missing ExactArgs), fzf-cross-01 (search 'matcher' missing FuzzyMatchV2). Partial: orca-search-04 — GetCurationHints now passes; findStaleNuggets still missing because "staleness" doesn't lexically appear near it. Likely needs semantic-fallback-on-existing-results for vague-concept queries.

✓ done
- orca-search-04 (partial): added FindEnclosingSymbol — preserves func/method enclosing scope, with doc-comment fallback (line+20). search 'curation' → GetCurationHints now resolves with receiver=(*Checker).
- orca-search-08: stale-spec fix (GoSymbolService refactored in orca)
- cobra-refs-01: real fix — FindRefs ORDER BY now biases def-file first

‡ score: 92% → 95.9% symbol accuracy after this session. 73 tasks scored, 4 failures + 3 known gaps.

‡ traps
- Eval uses `../cobra` and `../orca`, not `.eval-repos/`. Reproduce results from those paths.
- Search enrichment now prefers func/method/type/struct/interface over inner var/const. If a hit lands in a doc comment, falls back to nearest func/method/type whose line_start is within 20 lines after.
