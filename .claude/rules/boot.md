sha: ae492c9
updated: 2026-02-06T03:55:00Z
qa: pass
intent: make go_symbol delegate find operations to snipe CLI

ready: snipe-side Phase 0 complete — next work is in orca or new snipe features
- orca delegation already wired (go_symbol + index_handler) in prior orca session
- orca still has dead `internal/kits/code/gosym/` (3 files, zero imports) — delete it
- orca polish: surface Meta.StaleFiles, pass --max-tokens, forward suggestions
- snipe roadmap: context.Context in query layer (feat/query-context), issues #85-#88
- open issues: #85 M1 trust-the-index, #86 M2 harder-queries, #87 M3 LLM-output, #88 M4 distribution
- incremental indexing now works — test with real edits in orca repo

done:
- feat: incremental indexing (WriteIndexIncremental, filtered extraction, 50% threshold)
- ExtractRefsFiltered, ExtractCallGraphFiltered, ExtractImportsFiltered in index/
- incremental path in cmd/index.go: trySkipIndex returns changeDetection, runIncrementalIndex
- rg stderr capture for actionable error messages
- search.go error code fix: ErrRgNotFound vs ErrInternal
- scanner buffer in enrich.go for long lines
- 3 incremental store tests + 1 rg stderr test, mage qa passes

prior-session:
- Phase 0 version contract, suggestions, token budget, hex IDs, per-result staleness
- designed 5-phase plan (peppy-singing-valley), GitHub issues #85-#88
