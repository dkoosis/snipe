# arch — repo

run_id `ecebe5258308` · 46 pkgs, 81 import edges · 0 import cycles · 0 SCCs · no `.go-arch-lint.yml`

| dim | grade |
|---|---|
| Conformance | yellow (no layering config; 0 cycles) |
| Coupling | green (non-cmd pairs all ≤51 calls) |
| API Surface | red (3 pkgs >80 exports) |
| Pkg Health | yellow (no god-pkg, several oversized-surface) |
| Structural | green (no orphans, no reverse-DAG) |

**overall: red** — surface bloat drives the grade. Topology is clean (no cycles, no orphans, fan-in concentrated on `internal/util`/`output`/`store` as expected).

---

### 1. [F1] `internal/output/types.go` — pkg-surface-bloat

**Diagnosis.** `internal/output` exports 175 symbols across at least eight unrelated domains: Boundary*, DepTree*, Explain*, FileSummary, IndexState, KGHint, Caller*, Doc*, Error*, FuncAnalysis. The pkg is the "shared DTO dump" for everything that crosses the CLI seam.

**Why.** Readers cannot predict where a behavior lives; every cmd touches this pkg (Ca=5, PageRank #2 at 0.076). Any rename here ripples through `cmd/*` (441 calls) and `internal/query` (7 file-level edges). Refactor blast radius is the whole CLI.

**Evidence.** 175 exported symbols (sqlite count) · 2,468 LOC · Ca=5 · 441 cmd→output calls · file inventory: `human.go`, `json.go`, `lifecycle.go`, `types.go` — `types.go` alone holds dozens of unrelated structs.

**Fix.** Split along consumer-domain: `output/boundary`, `output/deps`, `output/explain`, `output/index` (state + errors), `output/lifecycle`. Keep `output` as the human/json renderer entry point only.

**Tier.** Red.

---

### 2. [F2] `internal/query/` — pkg-surface-bloat

**Diagnosis.** `internal/query` exports 155 symbols spanning boundary, deps, fuzzy, impact, imports, literals, lookup, position, resolve, signature, state, tests, trace, types_methods — 14 file-level domains in one import path.

**Why.** Query is the most-imported business pkg (Ca=5) and the second-largest LOC bucket (3,929). Its file structure already reveals the natural split. Any cmd adding a verb (e.g. `snipe impact`, `snipe boundary`) drags the whole pkg's transitive type cost.

**Evidence.** 155 exports · 3,929 LOC · Ca=5 · I=0.375 · 261 cmd→query calls · 51 query→store calls. Subdir-shaped file names confirm sub-domains.

**Fix.** Promote each verb-cluster to its own sub-pkg (`query/lookup`, `query/boundary`, `query/impact`, `query/trace`, `query/literals`). Leave `query` as a thin facade re-exporting only the types `cmd` truly needs.

**Tier.** Red.

---

### 3. [F3] `internal/context/` — pkg-surface-bloat

**Diagnosis.** `internal/context` exports 128 symbols mixing distinct domains: Architecture, Boundary, BuildInfo, CIInfo, CommandTable, Conventions, DBSchema, DataFlow, DepDAG, Nugget, Session, ProjectContext, Role, plus generators. Eleven domain families under one boundary.

**Why.** This pkg is the largest internal pkg by LOC (4,957) and second-largest by exports. The mix of "ProjectContext model" + "boot session state" + "convention detection" + "DB schema introspection" forces every consumer to know about every domain.

**Evidence.** 128 exports · 4,957 LOC · file families: `architecture.go`, `buildinfo.go`, `conventions.go`, `dbschema.go`, `flows.go`, `nug.go`, `roles.go`, `session.go`, plus `generate.go`/`format_text.go` renderers.

**Fix.** Carve into `context/model` (Project/Component/Boundary types), `context/session` (BootContext, ActiveWork), `context/probe` (buildinfo, dbschema, conventions detectors), `context/render` (format_text, generate). Keep `context` as the orchestration entry point only.

**Tier.** Red.

---

### 4. [F4] `internal/index/` — pkg-surface-bloat

**Diagnosis.** `internal/index` exports 77 symbols (orange band). At 2,075 LOC and Ca=4 (PageRank #3, 0.073), it's near the threshold where domain mixing starts to bite. Worth flagging now before it crosses 80.

**Why.** Sub-pkg `store` already depends on `index` (one file edge) and `query` depends on `index` (two edges); `embed` imports `index` too. Any sprawl here propagates fastest.

**Evidence.** 77 exports · 2,075 LOC · Ca=4 · I=0.2.

**Fix.** Audit exports — most index orchestration types likely belong unexported. Triage list: every `*Result`/`*Options` that exists for a single caller can move into that caller.

**Tier.** Yellow.

---

### 5. [F5] `internal/store/` — pkg-surface-bloat

**Diagnosis.** `internal/store` exports 68 symbols (orange) at 1,941 LOC. As the persistence boundary, it's the second-most-central pkg in the graph (PageRank 0.057, Ca=4). Surface size makes the seam wider than it needs to be.

**Why.** Persistence pkgs accumulate row types, query helpers, migration helpers, and connection plumbing. Wide surface = every consumer can reach into raw SQL. Narrower seam = easier to swap engines or add caching.

**Evidence.** 68 exports · 1,941 LOC · Ca=4 · 362 cmd→store calls · 51 query→store · 22 test/bench→store · 15 graphmetrics→store · 15 embed→store.

**Fix.** Demote internal helpers (row scanners, migration vehicles) to lowercase. Keep only the verbs consumers actually call (Open, Get*, Put*, Iter*) in the exported surface.

**Tier.** Yellow.

---

### 6. [F6] `.go-arch-lint.yml` (missing) — layering-violation

**Diagnosis.** No `.go-arch-lint.yml` or `.go-arch-lint-target.yml` exists. Intended layering is undocumented and unenforced.

**Why.** With 46 pkgs, 81 edges, and three red-surface hubs, future layering violations will be invisible until something cycles. The graph is currently acyclic and orderly (cmd→business→store/util) — exactly the moment to codify it.

**Evidence.** `ls .go-arch-lint*.yml` returns nothing. Cycles: 0 (clean baseline).

**Fix.** Add `.go-arch-lint.yml` with layers: `cmd` → {`context`, `query`, `lifecycle`, `embed`, `metrics`, `graphmetrics`, `diagram`, `edit`, `search`} → {`index`, `store`, `analyze`} → {`output`, `util`, `vector`, `config`, `kg`}. Mark current cross-layer edges as deps; new violations will fail CI.

**Tier.** Yellow.

---

### 7. [F7] `internal/embed/` — coupling-hotspot

**Diagnosis.** `internal/embed` has Ce=5 (depends on store, index, query, output, util, vector) and I=0.833 — high instability concentrated in one coordinator pkg. Below the Ce≥8 strict threshold but the highest non-cmd Ce in the repo.

**Why.** Embedding pipeline reaches into every layer (storage + symbol index + query + output rendering + vector store). Refactors in any of those four pkgs break embed. Classic "fragile coordinator" shape, just under the red line.

**Evidence.** Ce=5 · I=0.833 · 785 LOC · imports: index, output, query, store, util, vector (six pkgs, more than any non-cmd consumer).

**Fix.** Introduce an `embed/pipeline` step-runner type; have each phase take only its own port-interface (a `StoreWriter`, a `SymbolReader`) instead of importing the concrete pkgs. Inverts five of the six edges into one.

**Tier.** Yellow.

---

### 8. [F8] `internal/query` → `internal/store` (51 calls) — coupling-hotspot

**Diagnosis.** Query makes 51 calls into store across multiple files — exactly at the >50 hotspot threshold. Not a crisis but worth noting because query is itself in the red surface tier (F2).

**Why.** If query splits per F2, this coupling distributes naturally. If query doesn't split, this pair will keep growing each time a new lookup verb lands.

**Evidence.** sqlite call_graph join: 51 caller_id(query)→callee_id(store) call sites.

**Fix.** Bundled with F2 — once query becomes a facade over sub-pkgs, each sub-pkg holds its own narrower store dependency.

**Tier.** Yellow.

---

### 9. [F9] `internal/kg/` — lazy-package

**Diagnosis.** `internal/kg` has 136 LOC, Ca=1, exports a small handful of symbols, and is imported by only `cmd` (1 file edge). It's a thin boundary that buys little.

**Why.** kg looks like a placeholder for future Orca knowledge-graph integration but currently sits as an idle seam. Either grow it deliberately (with a roadmap commit) or fold it into the single caller.

**Evidence.** 136 LOC · Ca=1 · PageRank 0.023 · I=0.0 · imported only by `cmd` (per deps-tree).

**Fix.** Decide: if Orca KG work is imminent, leave it and add a `kg/README.md` noting the roadmap. Otherwise inline its types into `cmd/kg.go` or `internal/context/nug.go` (Nugget already lives in `context`).

**Tier.** Green.

---

### 10. [F10] `internal/vector/` — lazy-package

**Diagnosis.** `internal/vector` has 48 LOC, exports ~13 symbols, Ca=3 (used by store, embed, ?). Borderline lazy — the seam exists, but it's tiny.

**Why.** A 48-LOC pkg with three importers is right on the structural-payoff line. If the pkg's purpose is "vector type alias + a couple of helpers," it likely belongs inside `store/vector.go`.

**Evidence.** 48 LOC · 13 exports · Ca=3 · I=0.0 · PageRank 0.043 · imported by `embed` and `store` (2 distinct edges, plus its own test).

**Fix.** If vector ops are about to grow (real ANN, more vector kinds), keep the seam and add the planned types. If not, inline into `store` and drop a pkg from the graph.

**Tier.** Green.

---

**Notes / not flagged**

- `cmd/` (16 imports, 12,588 LOC, 441→output, 362→store, 261→query) is the composition root — exempt per rule catalog (`don't flag … deliberate composition roots in cmd/`). Still worth knowing it's the heaviest node by far.
- Orphans: SQL returned only `*.test` synthetic pkgs. No real orphan internal pkgs.
- Reverse-DAG: single-module repo, not applicable.
- Cycles: 0 SCCs in both call and import graphs.
- Per-function complexity, LCOM4, oversized-files: deferred to `/review clarity` and `/review cohesion` per linter scope.
