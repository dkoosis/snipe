sha: 50f0127
updated: 2026-02-06T01:40:00Z
qa: pass
intent: make go_symbol delegate find operations to snipe CLI

ready: snipe-side Phase 0 complete — next work is in orca or new snipe features
- orca delegation already wired (go_symbol + index_handler) in prior orca session
- orca still has dead `internal/kits/code/gosym/` (3 files, zero imports) — delete it
- orca polish: surface Meta.StaleFiles, pass --max-tokens, forward suggestions
- snipe roadmap: context.Context in query layer (feat/query-context), issues #85-#88
- open issues: #85 M1 trust-the-index, #86 M2 harder-queries, #87 M3 LLM-output, #88 M4 distribution

done:
- merged feat/version-contract to main (10 commits), deleted branch
- quick wins: suggestions in 7 commands, token budget in def/show, hex ID in refs/impl
- per-result file staleness (Meta.StaleFiles) in all 14 query commands + 9 unit tests
- fix: regenerate golden files for suggestions in def and show
- wrote orca developer integration statement (delegation status + recommended actions)

prior-session:
- Phase 0 version contract: protocol+ok envelope, version --json, lock file, doctor, embed tests, golden error tests
- designed 5-phase plan (peppy-singing-valley), reviewed by 3 LLMs, GitHub issues #85-#88
