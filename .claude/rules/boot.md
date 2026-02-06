sha: 9547a4b
updated: 2026-02-06T14:00:00Z
qa: pass
intent: make go_symbol delegate find operations to snipe CLI

ready: snipe-side usability complete — next work is orca delegation polish or new snipe features
- orca delegation already wired (go_symbol + index_handler) in prior orca session
- orca still has dead `internal/kits/code/gosym/` (3 files, zero imports) — delete it
- orca polish: surface Meta.StaleFiles, pass --max-tokens, forward suggestions
- snipe roadmap: context.Context in query layer (feat/query-context), issues #85-#88
- issues #85-#87 closed (delivered), #88 (distribution) open
- incremental indexing works — test with real edits in orca repo

done:
- --help grouping: Core/Index/Advanced command groups in root.go + 10 cmd files
- README rewrite: architecture diagram, embeddings rationale, full config reference, command tiers
- first-run UX: improved missing-index error message, quickstart guide in status when no index
- show.go moved from advanced to core group, improved help text for hex-ID chaining
- golden test + SPEC.md updated for new error message
- mage qa passes (all 7 stages green)

prior-session:
- incremental indexing, rg stderr capture, scanner buffer fix
- Phase 0 version contract, suggestions, token budget, hex IDs, per-result staleness
