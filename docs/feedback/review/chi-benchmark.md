# Chi Benchmark — snipe evaluation

Date: 2026-02-06
Target: go-chi/chi (HTTP router, 291 symbols, 755 refs, 201 calls)
Tester: Claude (external session)

## Scores

| Command | Score | Notes |
|---------|-------|-------|
| pack | 9 | Single-command complete picture. Best ROI command. |
| def | 9 | Fast, accurate, includes body + siblings. The workhorse. |
| types | 9 | Full method table with signatures = instant API surface. |
| callees | 8 | Accurate for direct calls. Interface dispatch gap is inherent. |
| refs | 8 | Comprehensive, shows enclosing function context. |
| impl | 7 | Correct results. Limited by static analysis. |
| show | 7 | Convenient hex ID recall from prior output. |
| context --boot | 7 | Good first-pass. 4/5 core types correct. Entry points weak. |
| explain | 6 | Quick purpose + mechanism, but pack is almost always better. |
| callers | 6 | Good for direct callers. Misses method-value references. |
| search | 5 | Same as rg but verbose JSON. edit_target metadata nice for programmatic use. |
| edit | 5 | Preview works. Diff output non-standard, hard to read. |
| pkg | 4 | Flat alphabetical dump of 50 items. Useless for orientation. |
| importers | 2 | Empty results for middleware→chi. Intra-module resolution broken. |

## Delight Moments

1. `pack routeHTTP` — full body + 2 refs + 5 callees + siblings in one call
2. `types Mux` — 32-method table with signatures, doc, embeds
3. Disambiguation: ambiguous ServeHTTP → both candidates with receiver info
4. `show <hex-id>` — recall symbol by ID avoids re-searching

## Frustration Moments

1. `pkg chi` — expected structured overview, got flat catalog
2. `importers` — middleware imports chi, got empty results (bug)
3. `callees ServeHTTP` — missed mx.handler.ServeHTTP() (interface dispatch)
4. `--include-body` flag doesn't exist on def (body is default, --no-body opts out)
5. JSON-only output noisy for human use

## Missing Capabilities Requested

1. pkg grouping: types → methods → standalone funcs, organized by role
2. Interface dispatch tracing (heuristic)
3. `snipe deps` — bidirectional package dependency graph
4. `snipe struct <name>` — struct fields with types (types shows methods, not fields)
5. Standard unified diff format for edit

## Key Insight

"Make snipe pkg actually useful for orientation. Types does this brilliantly for a single type; pkg should do the equivalent at package scale."

pkg is not "sort differently" — it's "types command scaled to package level."

## Maps to Ready List

- #1 types embed/field names "?" → struct fields listed as missing (data exists, display broken)
- #2 importers broken → intra-module resolution doesn't work at all
- #3 pkg grouping → loudest feedback signal (4/10)
