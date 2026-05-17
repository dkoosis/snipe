# domain-vocab — repo

run_id `ecebe5258308` · scope: project (cmd/, internal/) · vendor excluded

| dim | grade |
|---|---|
| P1 bool-trap | yellow (1 unexported func, but multi-bool methods rare) |
| P1 inline-func-type | green (only one minor inline callback; signatures stay clean) |
| P2 untyped-consts | red (≥3 call-site patterns pass bare bool literals across cmd/) |
| P2 vocab-drift | yellow (`internal/store` exports SQL-string-fragment param shape; `internal/output` exports broad cross-domain types — covered in arch review) |

**overall: yellow.** Surface looks disciplined at the symbol level, but several internal helpers have call sites where `false` / `false, false, 0` literals dominate the line. The dominant pattern is `ApplyFormatOverrides(format, withBody, false, contextLines)` — 11 sites, every one of them passes `false` as the unnamed `baseSiblings` bool.

---

### 1. [F1] `cmd/root.go:288` — magic-literal-at-call-site

**Diagnosis.** `ApplyFormatOverrides(format, baseBody, baseSiblings, baseContext)` has 11 call sites in `cmd/`, and 10 of them pass a bare `false` for `baseSiblings`. Readers of `cmd/sim.go:78`, `cmd/refs.go:60`, `cmd/callers.go:46`, etc. see `ApplyFormatOverrides(format, withBody, false, contextLines)` and have to chase the signature to learn what `false` means.
**Why.** The `false` is load-bearing for `FormatDefault` (it becomes the returned `withSiblings`), so callers can't just drop the arg — but the literal is the same in every cmd/ file. It is effectively a constant masquerading as a parameter. D1 says "default output = what Claude reads directly" — these call sites are read by Claude.
**Evidence.** `cmd/root.go:288` signature; sample call sites at `cmd/sim.go:78`, `cmd/refs.go:60`, `cmd/callees.go:46`, `cmd/callers.go:46`, `cmd/pkg.go:51`, `cmd/impl.go:43`, `cmd/tests.go:54`, `cmd/impact.go:55`, `cmd/search.go:57`, `cmd/def.go:58` (only `def.go` actually varies the bool). The one site that varies is `cmd/def.go:58` (`withBody, withSiblings, contextLines`).
**Fix.** Either split the helper (`ApplyFormatOverridesNoSiblings(format, baseBody, baseContext)` for the 10 sites that don't care; keep the full form for `def.go`), or pass a small options struct (`FormatBase{Body, Siblings, ContextLines}`). Both fixes turn the call sites into named adjustments instead of `false, false, 0` triples.

Tier: action

---

### 2. [F2] `internal/store/write.go:587` — magic-literal-at-call-site + vocab-drift

**Diagnosis.** `deleteByFilePaths(tx, selectQuery, deleteQuery string, files, repoRoot string)` takes two raw SQL fragment strings as parameters. Call sites at `internal/store/write.go:486` and `:491` read `deleteByFilePaths(tx, "SELECT id FROM symbols", "DELETE FROM embeddings WHERE symbol_id IN", allAffected, repoRoot)` — the SQL is a magic string baked into the call.
**Why.** The store pkg's vocabulary is supposed to be typed CRUD (Symbol, Ref, CallEdge). Passing SQL fragments at the API boundary inverts the abstraction: the caller authors SQL and the helper just executes it. The two existing call sites differ only in the target table — a typed enum (`type childTable int; const (childEmbeddings; childPurposes)`) would let the helper own the SQL and the call sites read `deleteByFilePaths(tx, childEmbeddings, allAffected, repoRoot)`.
**Evidence.** `internal/store/write.go:587` signature; `:486` and `:491` callers, both passing literal `"SELECT id FROM symbols"` plus a hardcoded `DELETE` fragment.
**Fix.** Replace the two `string` params with a typed `childTable` enum (or two named methods `deleteEmbeddingsForFiles` / `deletePurposesForFiles`). The `selectQuery` is invariant across both call sites — bake it into the helper. Eliminates the `#nosec G201` comment too.

Tier: action

---

### 3. [F3] `internal/output/json.go:47` — magic-literal-at-call-site

**Diagnosis.** `NewWriter(out io.Writer, compact bool, format OutputFormat)` is called 20+ times across `cmd/`. Two sites — `cmd/orient.go:75` and `cmd/pkg.go:228` — pass a bare `false` for `compact` while every other site passes a named `compact` variable. The bare `false` reads as if the caller forgot a flag.
**Why.** When 18 sites pass `compact` and 2 pass `false`, the inconsistency reads like a bug (did orient and pkg-callers forget to wire `--compact`?). Either both sites genuinely don't accept `--compact`, in which case a sibling constructor (`NewClassicWriter` / `NewExpandedWriter`) would say so, or they do accept it and the `false` is a latent bug.
**Evidence.** `internal/output/json.go:47` signature; `cmd/orient.go:75`, `cmd/pkg.go:228` (bare `false`); every other site passes the local `compact` flag.
**Fix.** Audit: do orient/pkg accept `--compact`? If yes, plumb the flag. If no, add `NewWriterExpanded(out, format)` so the call site documents its intent without the magic `false`.

Tier: action

---

### 4. [F4] `internal/query/tests.go:42` `internal/query/impact.go:39,155` — bool-trap-multi-arg (single-bool but cluster of three)

**Diagnosis.** `FindTests(db, symbolID, direct bool, limit, offset int)`, `FindTestsMulti(db, symbolIDs, direct bool, …)`, `FindImpactCallers(db, symbolID, direct bool, …)`, `FindImpactCallersMulti(db, …, direct bool, …)` — four related functions, all share a `direct bool` parameter, all called with bare locals like `testsDirect`, `impactDirect`. The bool itself isn't unreadable (the local name carries meaning), but it is the same modal flag re-implemented four times across two files.
**Why.** Per `bool-trap-multi-arg` rules: identical modal flag appearing across ≥3 sibling APIs is a candidate for a typed mode (`type Depth int; const (DepthDirect Depth = iota; DepthTransitive)`). Today, a reader of `FindImpactCallers(db, id, impactDirect, lim, 0)` sees three positional scalars and an opaque bool — typed `Depth` would make every call site self-document.
**Evidence.** `internal/query/tests.go:42`; `internal/query/tests.go:110`; `internal/query/impact.go:39`; `internal/query/impact.go:155`. Callers at `cmd/tests.go:149`, `cmd/impact.go:163`, `:165`, `:206`, `:208`.
**Fix.** Introduce `type SearchDepth int8` (Direct, Transitive) in `internal/query`. Migrate the four functions. Call sites become `query.FindTests(db, id, query.Direct, lim, off)`.

Tier: borderline

---

### 5. [F5] `cmd/sim.go:75` — bool-trap-multi-arg

**Diagnosis.** `runSimQuery(cmd, args, start, compact bool, lim, off, contextLines int, withBody bool)` — two bools and three ints jammed together. Only one call site (`cmd/sim.go:72`), but that site reads `runSimQuery(cmd, args, start, compact, lim, off, contextLines, withBody)` — five trailing scalars and the reader has to count positions to see which is which.
**Why.** Single internal caller blunts the urgency, but the function is the entry point for `snipe sim` and is likely to grow more flags. Once a sibling like `runSimPairs` (already exists at `cmd/sim.go:63`) starts sharing args, an options struct prevents drift.
**Evidence.** `cmd/sim.go:75` signature; `cmd/sim.go:72` call site.
**Fix.** Promote the trailing five args to a `simOpts` struct populated once from the cobra flag bindings, threaded into both `runSimQuery` and any future siblings.

Tier: borderline

---

### 6. [F6] `internal/index/loader.go:170` — inline-func-type-complex (borderline)

**Diagnosis.** `loadPackagesChunked(ctx, dir, fset, pkgPaths, chunkSize, tests bool, onProgress func(int, int)) (*LoadResult, error)`. The inline callback has 2 params (no return) — sits right at the threshold the linter flags. Only one caller (`loader.go:130`).
**Why.** `(int, int)` is opaque at the call site — what are the two ints? Reading the body shows `onProgress(end, total)` — current vs. total. A named alias (`type ProgressFunc func(done, total int)`) would name those at the type level, and would also be reusable when `cmd/index.go` grows a sibling progress hook for embeddings.
**Evidence.** `internal/index/loader.go:170`; `internal/index/loader.go:214` body call site `onProgress(end, total)`.
**Fix.** `type ProgressFunc func(done, total int)` in `internal/index`, used in `loadPackagesChunked` and in any future progress hook. Cheap; closes the door on `(int, int)` proliferating.

Tier: borderline

---

## Not flagged (deliberate)

- `walkGoFiles(dir, exclude, fn func(path string, d os.DirEntry) error)` — `filepath.WalkFunc`-shaped callback, idiomatic, two callers; aliasing would not pay for itself.
- `filterFlow(visited map[string]bool, edges, keep map[string]bool)` — `map[string]bool` is set semantics, not a bool-trap.
- `SetEnabled`/`SetShowSuggestions(v bool)` single-bool setters — self-documenting per the linter's `Don't` list.
- Package-name / pkg-cohesion issues for `internal/output` and `cmd/` — defer to `truthful-names` and the existing arch review (`docs/feedback/review/arch-repo.md` F1).
