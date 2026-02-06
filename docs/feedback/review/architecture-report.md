# Architecture Review

**Date:** 2026-02-06
**Snipe index:** 1,199 symbols, 4,734 refs, 1,321 call edges, 436 imports
**Scope:** internal/ (14 packages, 13,851 LOC non-test) + cmd/ (27 files, 7,210 LOC)
**Module:** github.com/dkoosis/snipe

---

## Conformance

**go-arch-lint:** No `.go-arch-lint.yml` configuration found. No enforced architectural rules exist.
**Target architecture:** No `.go-arch-lint-target.yml` found.
**Tech debt exclusions:** N/A

Without a go-arch-lint configuration, there is no machine-enforced layering. The dependency constraints are implicit, maintained only by convention.

---

## Dependency Topology

### Internal Dependency Graph

Edges between internal packages (cmd/ excluded):

```
metrics -> store, index, query, search  (Ce=4)
query   -> analyze, index, output       (Ce=3)
store   -> embed, index                 (Ce=2)
analyze -> output                       (Ce=1)
search  -> output                       (Ce=1)
index   -> util                         (Ce=1)
output  -> util                         (Ce=1)
```

Leaf packages (Ce=0): config, context, edit, embed, errors, kg, util

### Coupling Table (internal/ only)

| Package | Ca (fan-in) | Ce (fan-out) | Instability | Assessment |
|---------|-------------|--------------|-------------|------------|
| internal/index | 3 | 1 | 0.25 | Stable foundation |
| internal/output | 3 | 1 | 0.25 | Stable foundation |
| internal/util | 2 | 0 | 0.00 | Pure leaf |
| internal/embed | 1 | 0 | 0.00 | Pure leaf |
| internal/analyze | 1 | 1 | 0.50 | Balanced |
| internal/search | 1 | 1 | 0.50 | Balanced |
| internal/query | 1 | 3 | 0.75 | Unstable (low Ca mitigates) |
| internal/store | 1 | 2 | 0.67 | Unstable (low Ca mitigates) |
| internal/metrics | 0 | 4 | 1.00 | Fully unstable (orchestrator) |
| internal/config | 0 | 0 | -- | Isolated leaf |
| internal/context | 0 | 0 | -- | Isolated leaf (cmd-only) |
| internal/edit | 0 | 0 | -- | Isolated leaf (cmd-only) |
| internal/errors | 0 | 0 | -- | Orphan |
| internal/kg | 0 | 0 | -- | Isolated leaf (cmd-only) |

### Danger Zone Analysis

No packages fall in the danger zone (high Ca >= 10 AND high Instability >= 0.5). Maximum Ca is 3 (index, output). Expected for this codebase size.

### Cross-Package Call Coupling (internal/ only)

| Caller -> Callee | Calls |
|-----------------|-------|
| internal/metrics -> internal/store | 7 |
| internal/metrics -> internal/index | 4 |
| internal/metrics -> internal/query | 4 |
| internal/query -> internal/analyze | 4 |
| internal/store -> internal/embed | 3 |
| internal/index -> internal/util | 2 |
| internal/metrics -> internal/search | 2 |
| internal/output -> internal/util | 2 |

No pair exceeds 50 calls. Heaviest internal coupling is metrics->store at 7.

### cmd/ -> internal/ Call Coupling

| cmd/ -> Internal Package | Calls |
|--------------------------|-------|
| cmd -> internal/output | 316 |
| cmd -> internal/store | 181 |
| cmd -> internal/query | 122 |
| cmd -> internal/embed | 28 |
| cmd -> internal/index | 11 |
| cmd -> internal/metrics | 9 |
| cmd -> internal/context | 8 |
| cmd -> internal/edit | 6 |
| cmd -> internal/util | 2 |
| cmd -> internal/config | 1 |
| cmd -> internal/kg | 1 |
| cmd -> internal/search | 1 |

cmd/ imports 12 of the 14 internal packages. Heaviest: cmd->output (316 calls).

---

## API Surface

| Package | Exported | Funcs | Methods | Types | Vars | Consts | Tier |
|---------|----------|-------|---------|-------|------|--------|------|
| internal/output | 97 | 0 | 3 | 6 | 0 | 0 | RED (>80) |
| internal/context | 69 | 0 | 4 | 2 | 0 | 0 | ORANGE (41-80) |
| internal/query | 52 | 0 | 5 | 0 | 0 | 0 | ORANGE (41-80) |
| internal/index | 48 | 0 | 4 | 2 | 0 | 0 | ORANGE (41-80) |
| internal/embed | 35 | 0 | 14 | 0 | 0 | 0 | YELLOW (16-40) |
| internal/store | 29 | 0 | 20 | 0 | 0 | 0 | YELLOW (16-40) |
| cmd | 26 | 0 | 0 | 1 | 0 | 0 | YELLOW (16-40) |
| internal/metrics | 19 | 0 | 3 | 0 | 0 | 0 | YELLOW (16-40) |
| internal/edit | 14 | 0 | 2 | 1 | 0 | 0 | GREEN (<=15) |
| internal/util | 7 | 0 | 3 | 0 | 0 | 0 | GREEN (<=15) |
| internal/errors | 6 | 0 | 0 | 0 | 0 | 0 | GREEN (<=15) |
| internal/analyze | 5 | 0 | 1 | 0 | 0 | 0 | GREEN (<=15) |
| internal/config | 5 | 0 | 0 | 0 | 0 | 0 | GREEN (<=15) |
| internal/search | 4 | 0 | 0 | 0 | 0 | 0 | GREEN (<=15) |
| internal/kg | 3 | 0 | 0 | 0 | 0 | 0 | GREEN (<=15) |

### Observations

**internal/output (97 exports):** Largest API surface. 6 exported types plus 91 exported struct fields/methods/constants. The 97 exports on a 3-dependent package means many are consumed only by cmd/.

**internal/context (69 exports):** Only imported by cmd/. 69 exports for a single consumer is excessive.

**internal/query (52 exports), internal/index (48 exports):** Both above the review threshold. Cohesive but large.

### Interface Health

No interfaces defined in snipe source code. All type abstractions are concrete structs. Common for CLI tools but means no explicit contracts between layers.

---

## Package Health Heatmap

| Package | LOC | Files | Exports | Max File | I | Ca | Tier |
|---------|-----|-------|---------|----------|---|-----|------|
| internal/context | 3,292 | 10 | 69 | 863 | -- | 0 | RED |
| internal/output | 2,614 | 4 | 97 | 1,260 | 0.25 | 3 | RED |
| internal/query | 2,103 | 7 | 52 | 843 | 0.75 | 1 | RED |
| internal/index | 1,627 | 8 | 48 | 427 | 0.25 | 3 | YELLOW |
| internal/store | 1,380 | 4 | 29 | 697 | 0.67 | 1 | YELLOW |
| internal/embed | 703 | 3 | 35 | 441 | 0.00 | 1 | YELLOW |
| internal/metrics | 498 | 3 | 19 | 291 | 1.00 | 0 | YELLOW |
| internal/analyze | 682 | 2 | 5 | 374 | 0.50 | 1 | GREEN |
| internal/edit | 392 | 1 | 14 | 392 | -- | 0 | GREEN |
| internal/search | 178 | 1 | 4 | 178 | 0.50 | 1 | GREEN |
| internal/util | 121 | 1 | 7 | 121 | 0.00 | 2 | GREEN |
| internal/kg | 115 | 1 | 3 | 115 | -- | 0 | GREEN |
| internal/config | 110 | 1 | 5 | 110 | -- | 0 | GREEN |
| internal/errors | 25 | 1 | 6 | 25 | -- | 0 | GREEN |
| **cmd/** | **7,210** | **27** | **26** | **701** | -- | -- | **RED** |

### Red Package Details

1. **internal/context** (3,292 LOC, 10 files, 69 exports, max file 863)
   - RED on LOC (>3,000), RED on max file LOC (>800), ORANGE on exports
   - Files: generate.go (863), roles.go (441), flows.go (416), architecture.go (358), enrich.go (272), interfaces.go (231), ranking.go (191), session.go (178), nug.go (175), types.go (167)
   - Subdomain is coherent (LLM boot context) but size warrants splitting

2. **internal/output** (2,614 LOC, 4 files, 97 exports, max file 1,260)
   - RED on max file LOC (1,260 >> 800), RED on exports (97 > 80)
   - Files: gummy.go (1,260), json.go (685), types.go (570), human.go (99)
   - gummy.go is the single largest file in the codebase

3. **internal/query** (2,103 LOC, 7 files, 52 exports, max file 843)
   - RED on max file LOC (843 > 800), ORANGE on exports (52)
   - Files: lookup.go (843), explain.go (382), types.go (254), position.go (173), fuzzy.go (162), state.go (153), imports.go (136)

4. **cmd/** (7,210 LOC, 27 files, max 701)
   - RED on files (27 > 15), RED on LOC (>3,000)
   - Idiomatic for cobra-based CLI with many subcommands; not a structural problem

---

## Structural Findings

### Orphan Packages

1. **internal/errors** (25 LOC, 1 file)
   - Defines 6 sentinel errors (ErrSymbolNotFound, ErrAmbiguous, ErrIndexMissing, ErrIndexStale, ErrInvalidPosition, ErrInvalidSymbolID)
   - Zero imports from anywhere in the codebase
   - File: `/Users/vcto/Projects/snipe/internal/errors/errors.go`
   - Candidate for adoption or removal

2. **internal (root package)** -- `degradation.go` (11 LOC)
   - Contains single `CheckRg()` function
   - Not imported by any package
   - File: `/Users/vcto/Projects/snipe/internal/degradation.go`
   - Candidate for moving into internal/search or removal

### God Packages

No package meets strict god-package criteria (high Ca + high LOC + high exports simultaneously). Closest:
- internal/output: Ca=3, LOC=2,614, Exports=97 -- high exports but moderate Ca
- internal/context: LOC=3,292, Exports=69 but Ca=0 (only cmd/ imports it)

### Dependency Layering

The dependency graph forms a clean DAG with no cycles:

```
cmd/ (entry, composition root)
  |
  +-> context, edit, kg         (leaf packages, cmd-only consumers)
  +-> metrics -> store, index, query, search  (orchestrator)
  +-> query -> analyze, index, output
  +-> store -> embed, index
  +-> output, index -> util     (foundations)
```

No circular dependencies exist. Layering is sound.

### Notable Structural Patterns

1. **Flat cmd/ package**: All 27 command files in a single package with no subpackages. Idiomatic for cobra CLIs but means cmd/ acts as composition root with direct access to 12 internal packages.

2. **No interfaces**: Zero Go interfaces in internal/. All coupling through concrete types. Simplifies code but provides no seam for testing or implementation swapping.

3. **Heavy cmd/ -> output coupling** (316 calls): Every command constructs output types directly. Output envelope changes would require updating many command files.

4. **internal/metrics as pure orchestrator** (I=1.0, Ca=0): Imports 4 internal packages, consumed only by cmd/. Architecturally correct for metrics/comparison.

5. **internal/context isolation**: Largest package (3,292 LOC, 69 exports) with zero internal dependents. Only cmd/ imports it. Can be refactored without affecting any other internal package.

---

## File-Level Hotspots

Files exceeding 800 LOC (non-test):

| File | LOC | Package |
|------|-----|---------|
| `/Users/vcto/Projects/snipe/internal/output/gummy.go` | 1,260 | output |
| `/Users/vcto/Projects/snipe/internal/context/generate.go` | 863 | context |
| `/Users/vcto/Projects/snipe/internal/query/lookup.go` | 843 | query |

---

## Scorecard

| Dimension | Score | Notes |
|-----------|-------|-------|
| Conformance | N/A | No go-arch-lint config; no enforced rules |
| Coupling | GREEN | 0 danger-zone packages; max Ca=3; no tight coupling pairs; clean DAG |
| API Surface | RED | 1 red-tier package (output: 97 exports); 3 orange-tier (context, query, index) |
| Package Health | RED | 3 red-tier internal packages (context, output, query); 4 yellow-tier |
| Structural | YELLOW | 2 orphan packages (errors, degradation.go); no god packages; no cycles |
| **Overall** | **RED** | Driven by API surface breadth and package size |

### Interpretation

The snipe codebase has clean dependency topology -- no cycles, reasonable fan-in/fan-out, proper layering. But it suffers from oversized packages and wide API surfaces. The three red-tier packages (context, output, query) collectively represent 8,009 LOC (58% of internal/) and 218 exports.

The structural findings are minor (two small orphans). The absence of go-arch-lint configuration and Go interfaces means architectural constraints exist only by developer discipline.

### Suggested Nugs

| Priority | Finding | Suggested Nug |
|----------|---------|---------------|
| 1 | output/gummy.go at 1,260 LOC | kind: trap, name: "output/gummy.go oversized" |
| 2 | internal/context at 3,292 LOC with 10 files | kind: trap, name: "context package sprawl" |
| 3 | internal/errors orphaned (zero imports) | kind: trap, name: "errors package orphaned" |
| 4 | No go-arch-lint config | kind: pattern, name: "missing arch linting" |
| 5 | No interfaces in internal/ | kind: map, name: "no interface contracts" |
