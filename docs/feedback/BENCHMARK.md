# Snipe Quality Benchmark

Baseline established from 3 testing rounds: fzf, vhs, chi.
Use this to measure improvement and prevent regression.

## Command Scores (LLM tester, 1-10)

Target: every command >= 7. Commands below 7 need work before next testing round.

| Command | fzf | vhs | chi | Avg | Target | Blocker |
|-----------|-----|-----|-----|-----|--------|---------|
| def | 8 | 8 | 9 | 8.3 | 9 | body truncation in --human |
| callees | 9 | 9 | 8 | 8.7 | 9 | hold |
| refs | 8 | 8 | 8 | 8.0 | 8 | hold |
| callers | 5 | 5 | 9 | 6.3 | 7 | dynamic dispatch blind spot |
| types | - | 9 | 8 | 8.5 | 9 | --human broken |
| explain | - | 9* | - | 9.0 | 9 | --human broken; invisible to 2/3 Claudes |
| impl | 2 | n/a | 9 | 5.5 | 7 | heuristic fails on loose codebases |
| pack | 6 | 6 | 8 | 6.7 | 8 | structs return 0 callers/callees |
| show | 7 | 7 | 7 | 7.0 | 8 | hold |
| edit | - | 7 | 7 | 7.0 | 8 | --human broken; no visible diff |
| boot | 6 | 6 | 6 | 6.0 | 7 | ranking misses concrete types |
| pkg | 6 | 0 | 4 | 3.3 | 7 | flat dump; main pkg fails |
| search | 5 | 5 | 5 | 5.0 | 6 | little over rg |
| importers | 8 | 8 | 5 | 7.0 | 8 | short names don't resolve |

*explain scored 9 in JSON only; --human output was empty

## Quality Dimensions

### D1: Correctness — does the command return the right answer?

| Test Case | Current | Target |
|-----------|---------|--------|
| impl finds actual implementers (method-set match) | FAIL (fzf) | PASS |
| impl finds actual implementers (clean codebase) | PASS (chi) | PASS |
| pack on struct shows method-aggregated callers/callees | FAIL | PASS |
| callers finds dispatch-site for map-registered functions | FAIL | PASS |
| boot key_symbols excludes example/ and testdata/ | FAIL | PASS |
| pkg main resolves to correct package | FAIL | PASS |
| def main --file main.go returns main function | FAIL | PASS |

### D2: Completeness — does it return everything relevant?

| Test Case | Current | Target |
|-----------|---------|--------|
| boot key_symbols includes central concrete types | FAIL | PASS |
| boot key_symbols includes domain types (not just funcs) | FAIL | PASS |
| pack on struct lists its methods | FAIL | PASS |
| def shows full body for functions < 80 lines | FAIL | PASS |
| importers resolves short package names | FAIL | PASS |

### D3: Presentation — is output usable without post-processing?

| Test Case | Current | Target |
|-----------|---------|--------|
| explain --human renders all fields | FAIL | PASS |
| types --human renders method table | FAIL | PASS |
| edit --human renders diff | FAIL | PASS |
| pkg groups symbols by kind | FAIL | PASS |
| pkg summarizes constants (not enumerate) | FAIL | PASS |
| --human is default when stdout is TTY | FAIL | PASS |
| struct fields shown with --no-body | FAIL | PASS |

### D4: Discoverability — can the user find the right command/flag?

| Test Case | Current | Target |
|-----------|---------|--------|
| --human flag discovered without docs | FAIL (2/3) | PASS |
| edit flags obvious from --help | FAIL | PASS |
| --limit hint shown in truncation messages | FAIL | PASS |
| explain mentioned in suggestions after pack | PASS | PASS |

### D5: Self-sufficiency — task completed without fallback to other tools?

| Test Case | Current | Target |
|-----------|---------|--------|
| Orientation without reading files | partial | full |
| Understand function without Read fallback | FAIL (body trunc) | PASS |
| Trace call flow end-to-end | PASS | PASS |
| Assess blast radius of type change | PASS | PASS |
| Edit preview without JSON parsing | FAIL | PASS |

## Regression Guards

These work well today. Do not break them.

- def by name: fast, accurate, includes ref_count and hex ID
- callees chain: traces execution flow in 3-5 hops
- refs: instant blast radius with file grouping
- Qualified names (Mux.ServeHTTP) for disambiguation
- show by hex ID: reliable recall
- Disambiguation ("did you mean?") with candidates
- Suggestion field guiding next command
- Edit preview-by-default safety

## Automated Test Proposals

### Golden tests to add (test/blackbox/)

```
TestImpl_FindsImplementers_MethodSetMatch
  - fixture: interface with 2 implementations
  - assert: returns exactly the implementing types

TestPack_StructType_AggregatesMethodCallGraph
  - fixture: struct with 3 methods, each with callers
  - assert: pack result shows aggregated callers > 0

TestPkg_MainPackage_ReturnsSymbols
  - fixture: main package with exported types
  - assert: pkg "main" or pkg "." returns symbols

TestDef_BodyNotTruncated_SingleResult_Under80Lines
  - fixture: 50-line function
  - assert: body field contains full source, no "// ... truncated"

TestHumanFormat_ExplainResult_RendersAllFields
  - capture --human output for explain
  - assert: contains "purpose:", "mechanism:", "warnings:"

TestHumanFormat_TypesResult_RendersMethodTable
  - capture --human output for types
  - assert: contains method names and signatures

TestHumanFormat_EditResponse_RendersDiff
  - capture --human output for edit preview
  - assert: contains diff markers (+/-)

TestImporters_ShortName_ResolvesToFullPath
  - fixture: multi-package with imports
  - assert: importers "subpkg" finds importers
```

## Passive Telemetry Additions

Extend session.json to capture signal without user friction.

### 1. Command outcome tracking

Add to session.json per-query:
```json
{
  "symbol": "Evaluate",
  "command": "explain",
  "result_count": 1,
  "human_mode": false,
  "ms": 77,
  "body_truncated": true,
  "fallback": false
}
```

New fields:
- `human_mode`: whether --human was used (tracks adoption)
- `body_truncated`: whether body was cut (tracks the #1 complaint)
- `fallback`: set to true if same symbol queried with Read/cat within 30s (proxy for "snipe wasn't enough")

This stays local, best-effort, no behavior change.

### 2. Formatter coverage tracking

In the gummy formatter, log when a response type hits the fallback path:
```go
// In writeGenericResponse or equivalent:
if !hasSpecificFormatter {
    recordFormatterMiss(responseType)
}
```

Surface in `snipe status` as:
```
formatter coverage: 8/11 response types (missing: ExplainResult, TypesResult, EditResponse)
```

This makes the --human gap visible without testing.

### 3. Suggestions effectiveness

Track whether the user follows a suggestion:
```json
{
  "suggestion_shown": "snipe refs Evaluate",
  "suggestion_followed": true,
  "time_to_follow_ms": 4200
}
```

If suggestions are never followed, they're noise.
If specific suggestions are always followed, they should be automated.

## Active Feedback Solicitation

### Option A: Post-session summary (lightweight)

When `snipe` detects session end (no queries for 5 min, or explicit `snipe wrap`):
```
Session: 12 queries across 6 commands in 8 minutes
  Most used: def (4), callees (3), refs (2)
  Body truncated: 2 times
  Formatter fallback: 1 time (explain --human)

  Run `snipe feedback` to rate this session.
```

`snipe feedback` presents a minimal survey:
```
Which command was most useful this session?  [def/callees/refs/...]
Did you fall back to reading source?         [yes/no]
What was missing?                            [free text, optional]
```

Stored in `.snipe/feedback.jsonl`, one entry per session.

### Option B: LLM-specific feedback hook (for Claude/Orca integration)

Add a `feedback_request` field to the response JSON when snipe detects it's being called by an LLM (heuristic: no TTY on stdout, rapid sequential queries):

```json
{
  "results": [...],
  "meta": {...},
  "feedback_request": {
    "after_n_queries": 10,
    "questions": [
      "Which snipe command was most useful for your current task?",
      "Did any snipe output require you to fall back to file reading?",
      "What query would have helped that snipe doesn't support?"
    ]
  }
}
```

The LLM consumer (Orca's go_symbol handler) can choose to surface this or not.

### Option C: TESTING_PROMPT.md as structured benchmark (current approach, enhanced)

Keep the human-in-the-loop testing prompt but add structured scoring output:
```
After Phase 5, also run:
  snipe feedback --benchmark --session-file .snipe/session.json

This reads the session queries and generates:
  - Commands used vs. available (coverage)
  - Average result count per command
  - Body truncation rate
  - Formatter fallback rate
  - Comparison to previous benchmark run
```

## Improvement Actions (Priority Order)

### Wave 1: Presentation fixes (highest ROI, mechanical)

1. **Human formatters for ExplainResult, TypesResult, EditResponse** (#10)
   - Unblocks: explain, types, edit in --human mode
   - Lifts 3 commands from "broken" to 8-9/10
   - Effort: medium (add gummy renderers, ~200 LOC)

2. **Body truncation threshold** (#9)
   - Change gummyBodyPreview from 15 to 80 for single-result commands
   - No truncation when total results == 1
   - Effort: small (~20 LOC)

3. **--human as TTY default** (#8 partial)
   - isatty check on stdout, default to human when interactive
   - Effort: small (~15 LOC)

### Wave 2: Correctness fixes (algorithmic)

4. **pack on structs** (#1) — aggregate method call graphs
5. **pkg main resolution** (#11) — resolve "main" and "." to correct pkg_path
6. **boot context ranking** (#3 + #12) — type-aware scoring, exclude examples/
7. **importers short name resolution** — same pkg_path resolution as #11

### Wave 3: Quality improvements (design required)

8. **impl method-set matching** (#2) — replace file-cooccurrence heuristic
9. **pkg grouping** (#4) — organize by kind, summarize constants
10. **--no-body struct fields** (#5) — kind-aware flag behavior
11. **explain/types empty fallback** (#6) — graceful degradation

### Wave 4: Polish

12. **edit UX** (#7) — flag naming, success messages
13. **--limit hint in truncation** (#8 partial)
14. **search dedup** (#8 partial)
15. **callers dynamic dispatch awareness** — detect function value assignments
