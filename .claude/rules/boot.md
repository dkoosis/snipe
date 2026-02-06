sha: 3dbad39
updated: 2026-02-06T00:30:00Z
qa: pass
intent: make go_symbol delegate find operations to snipe CLI

ready: Phase 0 complete, merged to main
- plan at .claude/plans/peppy-singing-valley.md
- Phase 0 deliverables: all complete (version --json, ProtocolVersion, busy_timeout, StaleFiles)
- quality plan (phases 1-2) complete — 10 commits merged to main via feat/version-contract
- Phase 3 (context.Context in query layer) deferred to separate feat/query-context branch
- next: begin Phase 1 (snipe-first delegation in orca)

done:
- merged feat/version-contract to main (10 commits), deleted branch
- feat: wire suggestions in 7 commands (def, show, refs, callers, callees, search) + SuggestionsForCallees
- feat: token budget (--max-tokens) in def and show commands
- feat: hex ID auto-detection in refs and impl commands
- feat: per-result file staleness — Meta.StaleFiles in all 14 query commands + 9 unit tests
- removed dead store.GetFileMtimes (duplicated by query.queryFileMtimes)
- fix: exclusive lock file (O_CREATE|O_EXCL) prevents concurrent indexers @internal/store/store.go:120
- feat: 3-tier command visibility — 12 commands moved from Hidden to "Advanced Commands:" group
- feat: PRAGMA integrity_check in snipe doctor @cmd/doctor.go:134
- test: 22 unit tests for internal/embed/ (vector, client, batch) — was 0% coverage
- feat: NextAction.Description field + NewStaleIndexError constructor @internal/output/types.go:63
- test: 4 golden tests for error envelopes (NOT_FOUND, AMBIGUOUS, MISSING_INDEX, STALE_INDEX)
- fix: blackbox tests — allow protocol/ok keys, regenerate golden files
- fix: WriteError missing Protocol/Ok fields @internal/output/json.go:237

prior-session:
- designed 5-phase plan (peppy-singing-valley), reviewed by 3 LLMs, GitHub issues #85-#88
- baselines, snipe install, verified orca uses snipe from $PATH
