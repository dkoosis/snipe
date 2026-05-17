# slice-map — repo review

Scope: `internal/`, `cmd/` (vendor excluded). Linter: slice-map. Bundle: ecebe5258308.

Overall: the codebase is broadly disciplined — most `[]T`/`map[K]V` returns are freshly allocated inside DB-row loop builders, struct fields aren't handed out as long-lived shared views, and `append` sites build new slices rather than overwrite shared backing. Real findings cluster in (a) graph adjacency boundaries, (b) missed prealloc when length is computable, (c) nil-vs-empty inconsistency at small public APIs.

Tier: 🟡 — 5 P1/P2 findings worth fixing; no 🔴.

---

## 1. boundary-shares-backing — `graphmetrics.Graph.OutEdges` / `InEdges`

- **Site:** `internal/graphmetrics/graph.go:38`, `:43`
- **Issue:** `OutEdges` returns `g.Adj[n]` directly — the producer's internal map value. `InEdges` is computed fresh, so behavior is asymmetric.
- **Mutation impact:** every caller in this package (`scc.go`, `pagerank.go`, `betweenness.go`, `hits.go`, `topo.go`, `coupling.go`) currently uses the result as a read-only range target — safe today. But the type is exported (`Graph`, `Adj` are public) and the asymmetry with `InEdges` invites future callers to assume independence. A caller doing `out := g.OutEdges(n); out = append(out, x)` would silently mutate `g.Adj[n]` when capacity allows.
- **Code:**
  ```go
  // graph.go:38
  func (g *Graph) OutEdges(n string) []string {
      return g.Adj[n]
  }
  ```
- **Fix:** document the read-only contract on the method (`// callers MUST NOT mutate the returned slice`). The clone cost on every call inside Tarjan/PageRank/Brandes would matter; document, don't copy. Also document `Adj` itself since it's exported.

---

## 2. constructor-stores-input-slice — `analyze.NewAnalyzer`

- **Site:** `internal/analyze/warnings.go:23`
- **Issue:** stores caller-supplied `src []byte` in the struct without copying.
- **Mutation impact:** today the only caller (`internal/query/explain.go:93`) passes `src` it just read from disk and doesn't mutate after the call — safe. But the byte slice is mutable in principle and the analyzer reads from it across many method calls during AST walking; a caller that reused the buffer (pool-style) would silently corrupt evidence-line extraction.
- **Code:**
  ```go
  // warnings.go:15-28
  type Analyzer struct { fset *token.FileSet; src []byte; ... }
  func NewAnalyzer(fset *token.FileSet, src []byte, mode output.WarningsMode) *Analyzer {
      return &Analyzer{ fset: fset, src: src, mode: mode }
  }
  ```
- **Fix:** document "Analyzer takes ownership of src; caller must not mutate after construction." No copy needed for this use; source bytes are large and read-only by convention.

---

## 3. nil-vs-empty-mixed-returns — `analyze.Analyzer.AnalyzeFunc`

- **Site:** `internal/analyze/warnings.go:33-53`
- **Issue:** returns bare `nil` on the two early-return branches (`mode == WarningsNone`, `fn.Body == nil`), but on the normal path returns a `var warnings []output.Warning` that may itself be `nil` if all detectors return `nil` — or a non-nil empty slice if any detector ran with no findings (the appended sub-results all flatten to nil). Three distinct nil/empty shapes in one function. Callers in `cmd/` iterate with `range`, so behavior is the same — but consumers checking `== nil` vs `len() == 0` get inconsistent signals.
- **Code:**
  ```go
  func (a *Analyzer) AnalyzeFunc(fn *ast.FuncDecl) []output.Warning {
      if a.mode == output.WarningsNone { return nil }
      if fn.Body == nil { return nil }
      var warnings []output.Warning
      warnings = append(warnings, a.detectDeferInLoop(fn.Body)...)   // nil-safe
      // ...
      return warnings
  }
  ```
- **Fix:** document "returns nil when no warnings; never returns empty non-nil." Same convention is already implicit in `detectDeferInLoop` / `detectIgnoredError` (they return `nil` on no findings). One godoc line on the method.

---

## 4. missed-prealloc — `graphmetrics.Graph.InEdges`

- **Site:** `internal/graphmetrics/graph.go:43-58`
- **Issue:** `srcs := make([]string, 0, len(g.Adj))` is preallocated, but `var in []string` then appends one entry per matching source. Upper bound is `len(g.Adj)` (same as `srcs`), known up front.
- **Mutation impact:** none for correctness; just unnecessary reallocs in a hot path (called from PageRank's outer loop in `pagerank.go:39`, and HITS in `hits.go:41`). On large repos this runs `|nodes| * iter_count` times.
- **Code:**
  ```go
  func (g *Graph) InEdges(n string) []string {
      var in []string                          // ← prealloc
      srcs := make([]string, 0, len(g.Adj))
      for s := range g.Adj { srcs = append(srcs, s) }
      sort.Strings(srcs)
      for _, s := range srcs {
          for _, dst := range g.Adj[s] {
              if dst == n { in = append(in, s); break }
          }
      }
      return in
  }
  ```
- **Fix:** `in := make([]string, 0, len(g.Adj))` (worst-case bound; small overshoot OK). Better: cache an inverse adjacency once and serve `InEdges` from the cache — this method is O(V+E) per call and runs inside PageRank's k-iteration outer loop, so the algorithm is currently O(k·V·(V+E)) where it could be O(k·V + E).

---

## 5. missed-prealloc — `lifecycle.Classify`

- **Site:** `internal/lifecycle/classify.go:88`
- **Issue:** `order := make([]string, 0)` — capacity unset. The slice grows once per distinct enclosing-id, upper-bounded by `len(refs)`.
- **Mutation impact:** allocation count, not correctness. `Classify` is called once per `snipe lifecycle <T>` invocation, so it's not a hot path — but the rule is clear and the bound is trivially known.
- **Code:**
  ```go
  byEnc := make(map[string][]Ref)
  order := make([]string, 0)
  for _, r := range refs {
      if _, ok := byEnc[r.EnclosingID]; !ok {
          order = append(order, r.EnclosingID)
      }
      byEnc[r.EnclosingID] = append(byEnc[r.EnclosingID], r)
  }
  ```
- **Fix:** `order := make([]string, 0, len(refs))`. Same for `byEnc := make(map[string][]Ref, len(refs))` while you're there. Mirror sibling allocation at `:96`.

---

## 6. missed-prealloc — `context.generate.computeArchWarnings`

- **Site:** `internal/context/generate.go:176`
- **Issue:** `out := make([]ArchWarning, 0)` — `rows` is a known-size map; appendable upper bound is `len(rows)`.
- **Mutation impact:** allocation count only. Cold path (context generation, not query).
- **Code:**
  ```go
  out := make([]ArchWarning, 0)
  for pkg, r := range rows {
      if !r.hasA { continue }
      d := r.a + r.i - 1
      // ...
      out = append(out, ArchWarning{Pkg: pkg, A: r.a, I: r.i, D: d})
  }
  ```
- **Fix:** `out := make([]ArchWarning, 0, len(rows))`.

---

## 7. nil-vs-empty-mixed-returns — `context.queryCallGraph`

- **Site:** `internal/context/flows.go:158-173` (and twin `queryBoundaries` at `:296`)
- **Issue:** returns `map[string][]string` that may be empty-non-nil (the `make` allocates regardless of row count) but the function godoc doesn't state whether absence-of-key vs nil-slice-value have meaning. Consumers using `graph[id]` get a nil slice on miss (fine), but consumers iterating expect every key present in the map to have ≥1 entry — true by construction here, but not enforced or documented.
- **Mutation impact:** consumer-contract ambiguity; not corruption today.
- **Fix:** add godoc: "returns a non-nil map; absent keys = no callees. Empty values never appear."

---

## Skipped / not flagged

- `BootContext.ToNuggets` / `ProjectContext.ToNuggets` (`internal/context/nug.go`): build fresh `Nugget` values and fresh `[]string{}` tag slices each call — no shared backing.
- `Store.Read*` family (`internal/store/sccs.go`, `metrics.go`, `embed.go`, `write.go`): build fresh row slices inside the function and return them; standard idiom, no aliasing.
- `pack.go:170` `allDegraded = append(allDegraded, fmt.Sprintf(...))`: scalar append on a local accumulator; cap-retention non-issue (slice escapes only through return).
- `SymbolRow.computeHints` (`lookup.go:556`): uses `var hints []string`, returns possibly-nil — but the godoc convention is consistent ("returns hints, may be nil") and the field consumer treats nil as "no hints."
- `ExtractInterfaceMethodNames` (`lookup.go:1199`): documents nil return on error; consistent.

---

## Tier summary

| Tier | finding-class | count |
|------|---------------|-------|
| 🟡 | boundary-shares-backing (doc-fixable) | 1 |
| 🟡 | constructor-stores-input-slice (doc-fixable) | 1 |
| 🟡 | nil-vs-empty-mixed-returns | 2 |
| 🟡 | missed-prealloc | 3 |
| 🟢 | append-aliases / cap-retention / map-grow-during-iter | 0 |

Recommend: fix the three prealloc cases (1 line each), add godoc on the two boundary returns, document the call-graph map contract. The `Graph.InEdges` cached-inverse optimization is a separate perf task — file as its own issue if PageRank/HITS perf matters.
