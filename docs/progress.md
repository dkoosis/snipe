# Snipe — Progress

Cumulative record of what's been built. Append-only — never delete history.
Read first every session, then pick ONE task from boot.md.

## Project

Go code nav CLI for LLMs. Static indexing, <50ms queries, Claude-optimized output.
Goal: replace `go_symbol()` + Explore agents in orca.

Integration: orca calls snipe as subprocess. Commands consumed:
`def`, `refs`, `callers`, `callees`, `search`, `pack`, `show`, `context`.

---

## Phase 0: Foundation (complete)

Initial MVP through version contract.

| Task | Status | Notes |
|------|--------|-------|
| Phase 1 MVP: index, search, def, refs | done | 4bc8762 |
| Linting, mage build, tests | done | 543c348 |
| Protocol/ok fields, error envelopes | done | 50e31db–464ccd9 |
| Per-result file staleness via Meta.StaleFiles | done | 5732b95 |
| Suggestions, token budget, hex ID auto-detect | done | da429f7 |
| Version contract merge | done | 3dbad39 |

---

## Phase 1: Feature parity (complete)

Pack command, incremental indexing, scoped queries, role output.

| Task | Status | Notes |
|------|--------|-------|
| Incremental indexing + reliability | done | ae492c9 |
| Pack command, scoped queries, select flag, role output | done | c861dc6 |
| --help grouping, README rewrite, first-run UX | done | 9547a4b |
| Remove orphan internal/errors and internal/degradation | done | 6e730ec |
| Case-insensitive lookup, filename listing, receiver candidates | done | c9615f1 |
| Remove human output layer (LLM-only consumption) | done | 9fc3861 |
| Wave 1/2/3 correctness fixes from 3 rounds of LLM testing | done | 441c603–b5388fb |

---

## Phase 2: Reliability + eval (complete)

External audits, concurrency hardening, eval harness, search improvements.

| Task | Status | Notes |
|------|--------|-------|
| Go concurrency review (external) | done | PR #89 |
| High-quality Go tests (external) | done | PR #90 |
| SQLite expert audit (external) | done | PR #91 |
| SQLite pragma hardening, incremental write safety | done | 94df257 |
| Race condition fix in FileCache | done | dae6c73 |
| Eval harness: trustworthy scoring, YAML fixes | done | 3bdd0fa |
| Search filtering, range enrichment, expanded eval | done | 794cabb |
| Symbol accuracy: pkg sort, interface callers | done | 35c0cc7 |
| Anchor importer matching, dedupe batch IDs, stabilize ORDER BY | done | 1d4afcc |
| Add --no-follow to rg (macOS network volume fix) | done | 6fed2a5 |
| Auto-recover stale index lock files | done | 72ee38b |
| Eval baseline | done | 80.8% (engine-only, honest) |

Codex PR #92 reviewed — 9/12 recommendations rejected as generic. Closed.

---

## Phase 3: Simplification (complete)

28 dead symbols identified, ~800-1000 removable lines. Issues: #93, #94.

### Safe removals

| Task | Status | Notes |
|------|--------|-------|
| Dead store methods (5): GetMigrationHistory, SetPurpose, GetPurposeByHash, GetFileHash, DeleteEmbeddings | done | 90a2502, 7ae9f31 |
| Dead query exports (4): FindSymbolsByKind, FindImportsForDirectory, CountImportsByPackage, ResolveFileOrPackage | done | 8293259, 7ae9f31 |
| Dead cmd/output (4): GetSelectMode, ClearFileCache, NewStaleIndexError, PurposeFromLLM | done | 7ae9f31 |
| Dead edit: ValidOperations() | done | 7ae9f31 |
| fileExists() already uses os.Stat() | done | no shell-out found |
| Remove discarded fmt.Sprintf lint suppression | done | 7ae9f31 |
| Unused config: Config.Excludes, Config.IndexPath, GenerateConfig.IncludeArchSummary | done | 90a2502 |

### Enrichment stub cleanup

Decision: gut the stub, keep the schema.

| Task | Status | Notes |
|------|--------|-------|
| Remove EnrichSymbols() call from cmd/index.go | done | 7ae9f31 |
| Set --enrich default to false | done | 7ae9f31 |
| Delete dead Phase 3-4 exports: PruneOrphanedPurposes, FormatArchSummary | done | 7ae9f31 |
| Delete interfaces.go (231 lines, zero callers) | done | 7ae9f31 |
| Delete dead context exports (5): RankSymbolsByPackage, GetRoleWeight, InferRolesSummary, GetEntryPoints, GetAPIBoundaries | done | 7ae9f31 |
| Keep symbol_purposes table schema | — | documented TODO |
| Keep architecture.go data-gathering functions | — | sound, needed later |
| GetPurpose retained — used by pack command | — | not dead |

### Audit triage (2026-02-25)

Codex audit PR #95 reviewed — 5/7 findings real, 2 noise. Closed PR, extracted issues:

| Issue | Finding | Severity | Status |
|-------|---------|----------|--------|
| #96 | Batch embedding upload/download buffers entire payload in RAM | High | done 0bdfff9 |
| #97 | watch command lacks graceful shutdown (no ctx.Done) | Medium | done a776c5a |
| #98 | Missing rows.Err() in FindImplementers candidate scan | Medium | done de71af4 |

Dropped: embed-status ctx (by design for CLI), scanner 1MB cap (error caught, limit reasonable).

### Flags

Decision: keep but hide.

| Task | Status | Notes |
|------|--------|-------|
| MarkHidden --caller/--request-id, add "reserved for orca telemetry" comment | done | 7ae9f31 |

### Build fixes (2026-02-25)

| Task | Status | Notes |
|------|--------|-------|
| Scope gofmt to project dirs (was scanning .eval-repos) | done | 9512a0a |
| Remove contradictory --enable-only gosec from default mage target | done | 9512a0a |

---

## Upcoming (not started)

### Orca telemetry (gate for everything after)

Wire `persistToolCall` at orca `obs/telemetry/metrics/tool_telemetry.go:236`.
Plumbing exists: LogToolCallAsync → recordToolTelemetry → sqlite_writer INSERT.
Without ground truth telemetry, we can't measure whether changes help.

### Enrichment Phase 3: LLM-generated purposes

Only after telemetry confirms boot context is consumed. Plan: `docs/PLAN-context-enrichment.md`.
Wire real LLM call in generatePurpose() (haiku, ~$0.03/full index).
Content-hash incremental enrichment (schema already exists).

### Other

- #93 wildcard predicates
- #96 batch embedding OOM — done 0bdfff9
- #88 M4 distribution (hidden commands: schema, watch)
- #94 hash-based staleness — done 0c34b4a
- #99 interface dispatch filtering — done 0f33ada
- Enrichment Phase 4: architecture summary
- Eval improvements (blocked on telemetry)

---

## Stream E: Claude Orientation (in progress)

Improve snipe's usefulness for Claude navigating Go repos. Issues #105-#112.

| Issue | Feature | Effort | Impact | Status |
|-------|---------|--------|--------|--------|
| #105 | Package narratives from doc.go | S | High | done 5c6eaaf |
| #109 | Interface satisfaction map | S | High | done 5c6eaaf |
| #111 | Build/test system detection | S | High | done 5c6eaaf |
| #106 | Dependency topology (`snipe deps`) | M | High | done aa699a2 |
| #107 | Convention detection | M | Medium | — |
| #108 | Test mapping | M | Medium | — |
| #110 | Change impact surface | L | High | — |
| #112 | Token budget optimization | M | Medium | — |

Simplify review after quick wins: deduplicated interface method extraction, consolidated build detection, upsert for package_docs. Net -105 lines (c34271e).

---

## Output Format Rewrite (2026-03-06)

Default output changed from JSON envelope to Claude-optimized structured text.
~68% token reduction. JSON available via `--format json` for orca integration.

| Change | Commit | Notes |
|--------|--------|-------|
| Fuzzy method name resolution | 34dca70 | Bare name → method match across receivers (#120) |
| Claude-optimized default output | 083667f | D1/D4: text default, JSON opt-in |
| Decision table D1-D6 + guard-rails | 64df431 | Trixi-style, do-not-revisit |
| Issues #120-#123 | — | fuzzy names, search fallback, human format, root detection |

---

## Issue fixes (2026-03-19)

| Issue | Fix | PR | Notes |
|-------|-----|-----|-------|
| #123 | index: repo root detection causes misleading MISSING_INDEX errors | #130 | FindProjectRoot in internal/util, OpenStore resolves git root, doctor mismatch check, blackbox test |
| #121 | search: 0 results with no_index degradation after indexing | #131 | LookupByName fallback between rg and semantic, renamed no_index → rg_only |
| — | simplify review | c344076 | search.go resolves git root for store, cached Exists in OpenStore, renamed usedIndexFallback → indexFallbackFound |

---

## Context output quality (2026-04-16)

| Change | Commit | Notes |
|--------|--------|-------|
| Command purposes, flow diversity, callee ranking | ad42a17 | context.go enrichment improvements |
| Test exclusion, placeholder purposes, truncation | 76c518e | cleaner boot context output |
| format_text.go: compact claudish rendering | 5bcb91a | no markdown tables, emDash→∅ |
| LLM-optimal defaults (#136) | 5bcb91a | --limit 10, --context 2, --format concise; pack sub-limits 5/5/5 |
| def: preserve body with --format concise | 5bcb91a | def always shows body unless --no-body |
| Closed #134 | — | --boot→--orient already done in code |

---

## Decisions log

Canonical decision table now in CLAUDE.md (D1-D6). Below is history.

| Decision | Rationale | Date |
|----------|-----------|------|
| D1: Claude is primary consumer | Default output optimized for Claude, not JSON parsers | 2026-03-06 |
| D4: Token budget is first-class | Every output byte must earn its place | 2026-03-06 |
| mage for build; mage qa is merge gate | Single command, Go-native | early |
| Output: {results, meta, error} JSON envelope | Superseded by D1 — JSON now opt-in via --format json | Phase 0 |
| Hex IDs: 16-char, chain across commands | Compact, unambiguous | Phase 0 |
| Remove human output layer | snipe is LLM-only tool | 2026-02 |
| Eval: engine-only scoring, no Claude self-assessment | Honest baselines | 2026-02 |
| Don't optimize eval until telemetry provides ground truth | Avoid overfitting to synthetic harness | 2026-02 |
| Enrichment: gut stub, keep schema + data functions | Stop fake work; infrastructure is sound for later | 2026-02-24 |
| --caller/--request-id: hide, don't delete | Dead today, critical path for orca telemetry | 2026-02-24 |
| Hidden commands (schema, watch): keep | Noted on #88, dependencies acceptable | 2026-02 |
