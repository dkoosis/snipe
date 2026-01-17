# Snipe Work Plan: Issues → Nugs + Parallel Execution

## Overview

27 work items (15 GitHub issues + 12 REVIEW.md recommendations) consolidated into **28 unique tasks**, organized for parallel Claude execution with explicit conflict avoidance and verification gates.

---

## Revision Notes

1. **Moved `snipe-limit-ambiguous`** from Stream F → Stream A (SPEC determinism contract)
2. **Moved `snipe-context-cmd`** from Stream E → Stream C (enables verification)
3. **Added** `snipe-config-load`, `snipe-doctor`, `snipe-golden-tests`, `snipe-sqlite-driver`
4. **Added** conflict hotspot map, DoD per task, schema migration contract, phase gates

---

## Conflict Hotspot Map

These files are touched by multiple tasks. **Single-writer at a time** policy:

| File | Tasks That Touch It | Merge Order |
|------|---------------------|-------------|
| `internal/store/schema.go` | sqlite-driver, fix-like, schema-indexes | A → C |
| `internal/index/refs.go` | fix-poskey, enclosing-perf, file-cache | A → C |
| `internal/index/loader.go` | fix-exclude, chunked-load | A → C |
| `internal/search/rg.go` | scanner-limits, rg-exitcodes | B (either order) |
| `internal/query/lookup.go` | fix-like, limit-ambiguous | A (sequential) |
| `internal/output/types.go` | golden-tests, content-hash, suggestions | B → D |

**Rule:** If your task touches a hotspot file, check the merge order. Wait for upstream tasks to land before starting work that modifies the same file.

---

## Schema Migration Contract

### The `file_path_rel` Column Change (task: `snipe-fix-like`)

**Problem:** Current queries use `LIKE '%path%'` which is slow and ambiguous.

**Solution:** Add `file_path_rel TEXT` column storing repo-relative paths.

**Migration Steps:**
1. Add column as nullable: `ALTER TABLE refs ADD COLUMN file_path_rel TEXT`
2. Backfill from existing `file_path` by stripping repo root
3. Add index: `CREATE INDEX idx_refs_path_rel ON refs(file_path_rel)`
4. Update queries to use exact match on `file_path_rel`
5. Make column NOT NULL in future schema version

**Compatibility:**
- Forward: New code works with old indexes (falls back to LIKE)
- Backward: Not supported. Old code ignores new column.
- **Reindex required:** Yes, full reindex after schema change

**Schema Version:** Bump from `1` → `2` in `meta` table

**Rollback:** Drop column, revert queries to LIKE. Requires reindex.

---

## Smoke Suite (must pass every PR)

```bash
# ~10 second smoke test
cd testdata/fixture  # tiny Go repo checked into repo
snipe index .
snipe def ProcessOrder --json | jq '.results[0].file'
snipe refs --at handler.go:15:4 --json | jq '.meta.total'
snipe search "TODO" --json | jq '.results | length'
snipe doctor --json | jq '.ok'
```

**Expected outputs:**
- `def`: returns file path string
- `refs`: returns integer ≥ 0
- `search`: returns integer ≥ 0
- `doctor`: returns `true`

Add to CI: `mage smoke` target.

---

## Branch/PR Protocol

| Rule | Detail |
|------|--------|
| Branch naming | `snipe/<nug-slug>` e.g. `snipe/fix-poskey` |
| Scope | One branch per task. No drive-by refactors. |
| PR checklist | DoD items, files touched, schema impact (yes/no) |
| Merge policy | Rebase onto main before merge. Squash optional. |
| Hotspot files | Coordinate via PR comments if touching same file |

**PR Template:**
```markdown
## Task
`n:task:snipe-<slug>`

## DoD Checklist
- [ ] Item 1
- [ ] Item 2

## Files Touched
- `internal/foo/bar.go`

## Schema Impact
- [ ] Yes (describe migration)
- [x] No
```

---

## Phase Gates (Concrete Conditions)

### Phase 1 → Phase 2 Gate

**Condition:** Phase 2 (Stream C) starts when ALL of:
1. `snipe-fix-poskey` merged
2. `snipe-fix-exclude` merged
3. `snipe-fix-like` merged (schema v2 in place)
4. `snipe-golden-tests` merged and passing
5. Fresh reindex on test fixture passes
6. `mage smoke` passes

### Phase 2 → Phase 3 Gate

**Condition:** Phase 3 (Streams D+E) starts when ALL of:
1. All Stream C tasks merged
2. `snipe context .` produces valid JSON on test fixture
3. No OOM on medium repo (10k LOC) with chunked loading
4. `mage qa` passes

---

## Task Inventory with DoD

### Stream A: Core Correctness (blocking)

#### `n:task:snipe-sqlite-driver`
**Task:** Settle SQLite driver: modernc.org vs go-sqlite3

**DoD:**
- [ ] ADR document written with decision rationale
- [ ] go.mod updated to chosen driver
- [ ] `mage build` produces single binary without CGO
- [ ] Cross-compile test: `GOOS=linux GOARCH=amd64 mage build` succeeds
- [ ] Existing tests pass

**Files:** `go.mod`, `internal/store/store.go`, `docs/adr/`

---

#### `n:task:snipe-fix-poskey`
**Task:** Fix position-key construction - replace `string(rune)` with `fmt.Sprintf`

**DoD:**
- [ ] `string(rune(line))` replaced with `fmt.Sprintf("%d", line)`
- [ ] Unit test added for position key generation
- [ ] `mage test` passes
- [ ] Reindex produces same symbol count (no data loss)

**Files:** `internal/index/refs.go`

---

#### `n:task:snipe-fix-exclude`
**Task:** Fix exclusion matching - use path component split

**DoD:**
- [ ] `filepath.SplitList` replaced with `strings.Split(path, "/")`
- [ ] Test: path `vendor/foo/bar.go` excluded when `vendor` in exclude list
- [ ] Test: path `myvendor/foo.go` NOT excluded (partial match rejection)
- [ ] `mage test` passes

**Files:** `internal/index/loader.go`

---

#### `n:task:snipe-fix-like`
**Task:** Remove `LIKE '%path%'` queries

**DoD:**
- [ ] `file_path_rel` column added to schema
- [ ] Migration backfills existing rows
- [ ] Schema version bumped to 2
- [ ] All `LIKE '%path%'` replaced with exact match
- [ ] Query benchmark: exact match ≥2x faster than LIKE on 10k refs
- [ ] `mage test` passes
- [ ] Smoke suite passes after reindex

**Files:** `internal/store/schema.go`, `internal/query/lookup.go`, `internal/query/position.go`

**Schema Impact:** Yes (see Migration Contract above)

---

#### `n:task:snipe-limit-ambiguous`
**Task:** Return `candidates[]` on ambiguity per SPEC

**DoD:**
- [ ] When multiple symbols match bare name, return `error.code: "AMBIGUOUS_SYMBOL"`
- [ ] Response includes `error.candidates[]` array
- [ ] Test: `snipe def Config` with 2 Config types returns candidates
- [ ] JSON output matches SPEC example exactly
- [ ] `mage test` passes

**Files:** `internal/query/lookup.go`, `internal/output/types.go`

---

### Stream B: Reliability (parallel to A)

#### `n:task:snipe-scanner-limits`
**Task:** Increase bufio.Scanner buffer, check scanner.Err()

**DoD:**
- [ ] `scanner.Buffer(buf, 1024*1024)` added (1MB limit)
- [ ] `scanner.Err()` checked after scan loop
- [ ] Test: file with 500KB line doesn't panic
- [ ] `mage test` passes

**Files:** `internal/search/rg.go`

---

#### `n:task:snipe-rg-exitcodes`
**Task:** Handle rg exit codes properly

**DoD:**
- [ ] Exit code 1 (no matches) returns empty results, not error
- [ ] Exit code 2+ propagates as error with stderr message
- [ ] Test: search for nonexistent pattern returns `results: []`
- [ ] Test: search with invalid regex returns error
- [ ] `mage test` passes

**Files:** `internal/search/rg.go`

---

#### `n:task:snipe-sqlite-locks`
**Task:** Add busy_timeout and connection limits

**DoD:**
- [ ] `PRAGMA busy_timeout = 5000` executed on open
- [ ] `db.SetMaxOpenConns(1)` called
- [ ] Test: concurrent read/write doesn't deadlock (10 goroutines)
- [ ] `mage test` passes

**Files:** `internal/store/store.go`

---

#### `n:task:snipe-config-load`
**Task:** Implement global/project config merging

**DoD:**
- [ ] Loads `~/.config/snipe/config.json` if exists
- [ ] Loads `.snipe.json` in project root if exists
- [ ] Project config overrides global config
- [ ] Test: merged config has correct precedence
- [ ] `mage test` passes

**Files:** `internal/config/config.go` (new), `cmd/root.go`

---

#### `n:task:snipe-doctor`
**Task:** Implement snipe doctor command

**DoD:**
- [ ] Checks rg availability, reports version or install instructions
- [ ] Checks index existence and freshness
- [ ] Returns JSON: `{"ok": bool, "checks": [...]}`
- [ ] Test: doctor with missing rg returns `ok: false`
- [ ] `mage test` passes

**Files:** `cmd/doctor.go` (new)

---

#### `n:task:snipe-golden-tests`
**Task:** Snapshot testing for JSON output

**DoD:**
- [ ] Golden files added for: def, refs, callers, search output
- [ ] Test compares actual output to golden files
- [ ] `go test -update` flag regenerates golden files
- [ ] CI fails if output schema drifts
- [ ] `mage test` passes

**Files:** `internal/output/golden_test.go` (new), `testdata/golden/`

---

### Stream C: Performance + Context (after Phase 1 gate)

#### `n:task:snipe-enclosing-perf`
**Task:** Replace O(N*M) findEnclosing with AST walker

**DoD:**
- [ ] AST walker maintains function stack during traversal
- [ ] Enclosing lookup is O(1) per reference
- [ ] Benchmark: 10k refs indexed in <5s (was ~15s)
- [ ] `mage test` passes
- [ ] Golden tests still pass

**Files:** `internal/index/refs.go`, `internal/index/callgraph.go`

---

#### `n:task:snipe-file-cache`
**Task:** Cache file contents during indexing

**DoD:**
- [ ] File content cached in memory during single index run
- [ ] Cache cleared after indexing completes
- [ ] Benchmark: repeated file reads reduced by 80%+
- [ ] `mage test` passes

**Files:** `internal/index/refs.go`

---

#### `n:task:snipe-schema-indexes`
**Task:** Add composite indexes

**DoD:**
- [ ] Index added: `refs(file_path_rel, line, col)`
- [ ] Index added: `symbols(file_path, line_start)`
- [ ] Query benchmark: `refs --at` ≥2x faster
- [ ] `mage test` passes

**Files:** `internal/store/schema.go`

---

#### `n:task:snipe-chunked-load`
**Task:** Chunked package loading to prevent OOM

**DoD:**
- [ ] Packages loaded in batches of 50
- [ ] Memory usage <500MB on 50k LOC repo
- [ ] Test: index large-repo fixture without OOM
- [ ] `mage test` passes

**Files:** `internal/index/loader.go`

---

#### `n:task:snipe-context-cmd`
**Task:** Implement snipe context command

**DoD:**
- [ ] `snipe context [path]` outputs JSON per SPEC
- [ ] `--format=yaml` flag works
- [ ] `--full` includes all symbols
- [ ] Output includes: project, architecture, files, symbols, meta
- [ ] `mage test` passes
- [ ] Smoke: `snipe context . | jq '.project.name'` returns repo name

**Files:** `cmd/context.go` (new), `internal/context/` (new)

---

### Stream D: Features (after Phase 2 gate)

| Nugget ID | Task | DoD Summary |
|-----------|------|-------------|
| `snipe-max-tokens` | --max-tokens flag | Flag parsed, output truncated at token limit, test passes |
| `snipe-content-hash` | content hash in edit_target | SHA256 of line range in edit_target, test passes |
| `snipe-schema-cmd` | snipe schema command | Outputs JSON Schema for Response type, test passes |
| `snipe-semantic-truncation` | Semantic truncation | Truncates at statement boundaries, test passes |
| `snipe-relevance-scoring` | Relevance scoring | Results sorted by score, scoring documented, test passes |
| `snipe-suggestions` | suggestions field | Output includes `suggestions[]`, test passes |

---

### Stream E: Major Features (parallel to D)

#### `n:task:snipe-mcp-server` — Split into slices:

| Slice | Scope | DoD |
|-------|-------|-----|
| E1-scaffold | CLI flag `--mcp`, stdio transport setup | `snipe --mcp` starts, accepts JSON-RPC |
| E1-tools | Expose def/refs/search as MCP tools | Claude Code can call tools |
| E1-harden | Error handling, timeouts, docs | Production-ready, README updated |

#### `n:task:snipe-treesitter` — Split into slices:

| Slice | Scope | DoD |
|-------|-------|-----|
| E2-scaffold | tree-sitter-go binding, fallback flag | `--fallback-parser` flag exists |
| E2-parse | Parse broken Go files, extract symbols | Broken file returns partial symbols |
| E2-integrate | Integrate with main indexer | Index completes even with parse errors |

---

### Stream F: Backlog

| Nugget ID | Task | DoD Summary |
|-----------|------|-------------|
| `snipe-sig-formatting` | Tighten signatures | Consistent formatting, test passes |
| `snipe-cancellation` | Context/cancellation | Long ops respect ctx.Done(), test passes |
| `snipe-static-hints` | Static analysis hints | Output includes hints[], test passes |
| `snipe-quality-metrics` | Search quality metrics | Metrics logged, dashboard exists |

---

## Execution Dashboard

| Task | Stream | Status | Assignee | PR | Blocked By |
|------|--------|--------|----------|----|-----------|
| sqlite-driver | A | todo | - | - | - |
| fix-poskey | A | todo | - | - | sqlite-driver |
| fix-exclude | A | todo | - | - | - |
| fix-like | A | todo | - | - | - |
| limit-ambiguous | A | todo | - | - | fix-like |
| scanner-limits | B | todo | - | - | - |
| rg-exitcodes | B | todo | - | - | - |
| sqlite-locks | B | todo | - | - | sqlite-driver |
| config-load | B | todo | - | - | - |
| doctor | B | todo | - | - | config-load |
| golden-tests | B | todo | - | - | - |
| enclosing-perf | C | todo | - | - | Phase 1 Gate |
| file-cache | C | todo | - | - | Phase 1 Gate |
| schema-indexes | C | merged | #47 | - | fix-like |
| chunked-load | C | merged | #46 | - | Phase 1 Gate |
| context-cmd | C | merged | #45 | - | Phase 1 Gate |
| max-tokens | D | merged | #48 | - | Phase 2 Gate |
| content-hash | D | merged | #49 | - | Phase 2 Gate |
| schema-cmd | D | merged | #50 | - | Phase 2 Gate |
| semantic-truncation | D | merged | #52 | - | Phase 2 Gate |
| relevance-scoring | D | merged | #53 | - | Phase 2 Gate |
| suggestions | D | merged | #54 | - | Phase 2 Gate |
| mcp-server | E | dropped | #51→reverted | - | Complexity outweighed value for CLI-first tool |
| treesitter | E | todo | - | - | Phase 2 Gate |

**Status values:** `todo` | `in-progress` | `blocked` | `in-review` | `merged`

---

## Integration Sheriff

**Role:** One person/agent per phase responsible for:
- Merge order decisions
- Conflict resolution on hotspot files
- Enforcing invariants (schema version, output format)
- Updating this plan as tasks land

**Assignment:**
- Phase 1: TBD
- Phase 2: TBD
- Phase 3: TBD

---

## Cross-Cutting Spec Sync Checklist

When completing a task, check if SPEC.md needs updates:

- [ ] **Output schema change** → Update "Result Schema" section
- [ ] **New command/flag** → Update "Commands" section
- [ ] **Indexing change** → Update "Index Schema" or "Index Invalidation"
- [ ] **New error code** → Update "Input Addressing" error examples
- [ ] **Degradation change** → Update "Graceful Degradation" table
- [ ] **Config change** → Update "Configuration" section

---

## Dependency Graph (Detailed)

```
                    ┌─────────────────────────────────────┐
                    │         STREAM A (Correctness)      │
                    │                                     │
                    │  sqlite-driver ←─────────────────┐  │
                    │       │                          │  │
                    │       ▼                          │  │
                    │  fix-poskey    fix-exclude       │  │
                    │       │             │            │  │
                    │       └──────┬──────┘            │  │
                    │              ▼                   │  │
                    │         fix-like ────────────────┤  │
                    │              │                   │  │
                    │              ▼                   │  │
                    │      limit-ambiguous             │  │
                    └──────────────┬───────────────────┘  │
                                   │                      │
    ┌──────────────────────────────┼──────────────────────┘
    │                              │
    │  ┌───────────────────────────┴───────────────────┐
    │  │           STREAM B (Reliability)              │
    │  │  (runs parallel to A, no dependencies on A)   │
    │  │                                               │
    │  │  scanner-limits    rg-exitcodes               │
    │  │        │                │                     │
    │  │        └────────┬───────┘                     │
    │  │                 ▼                             │
    │  │  sqlite-locks ←── (needs sqlite-driver)       │
    │  │        │                                      │
    │  │        ▼                                      │
    │  │  config-load ───► doctor                      │
    │  │        │                                      │
    │  │        ▼                                      │
    │  │  golden-tests ◄─── GATE: must pass            │
    │  └───────────────────────┬───────────────────────┘
    │                          │
    └──────────────────────────┼───────────────────────
                               │
                    ═══════════╪═══════════════════════
                        PHASE 1 → PHASE 2 GATE
                    ═══════════╪═══════════════════════
                               │
                    ┌──────────▼──────────────────────┐
                    │    STREAM C (Performance)       │
                    │                                 │
                    │  enclosing-perf   file-cache    │
                    │        │              │         │
                    │        └──────┬───────┘         │
                    │               ▼                 │
                    │  schema-indexes ←── (after fix-like) │
                    │               │                 │
                    │               ▼                 │
                    │  chunked-load   context-cmd     │
                    └──────────────┬──────────────────┘
                                   │
                    ═══════════════╪═══════════════════
                        PHASE 2 → PHASE 3 GATE
                    ═══════════════╪═══════════════════
                                   │
                    ┌──────────────┴──────────────────┐
                    │                                 │
            ┌───────▼───────┐             ┌──────────▼─────────┐
            │  STREAM D     │             │    STREAM E        │
            │  (Features)   │             │  (Major Features)  │
            │  6 tasks      │             │  2 tasks (sliced)  │
            └───────────────┘             └────────────────────┘
```

---

## Task Count Summary

| Stream | Count | Parallel Slots |
|--------|-------|----------------|
| A | 5 | 1-2 (some sequential) |
| B | 6 | 2-3 (mostly parallel) |
| C | 5 | 1-2 (some sequential) |
| D | 6 | 3+ (all parallel) |
| E | 2 (6 slices) | 2 (all parallel) |
| F | 4 | backlog |
| **Total** | **28** | |
