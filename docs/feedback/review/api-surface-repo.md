# api-surface — repo review

Date: 2026-05-17 · Run: ecebe5258308 · Scope: project (read-only)

Pre-work: 1 exported interface (`embed.Embedder`), 1 non-vendor embedded type
(`query.TraceRef` embeds `LiteralRef`, methodless — no leak). 23 distinct
method-receiver shapes; all but `Coupling` are pointer — no receiver-mix.

Findings: 4 (3 action, 1 borderline).

---

### 1. [F1] `internal/embed/search.go:22` — single-impl-interface

**Diagnosis:** `Embedder` interface has exactly one production implementer
(`*embed.Client`) and one `_test.go` mock (`mockEmbedder` in
`internal/embed/search_test.go:17`). Three call sites all pass `*Client`.

**Why:** Per `single-impl-interface`, test-only mocks don't count as a second
impl. The interface buys nothing the concrete type doesn't already give; it
adds a layer readers must traverse to reach `EmbedOne`'s real body.

**Evidence:**
- decl: `internal/embed/search.go:22` — `type Embedder interface { EmbedOne(...) ([]float32, error) }`
- prod impl: `internal/embed/client.go:215` — `func (c *Client) EmbedOne(...)`
- test mock: `internal/embed/search_test.go:17` — `mockEmbedder`
- callers (3, all `*Client`): `cmd/search.go:135`, `cmd/search.go:243`, `cmd/sim.go:100`

**Fix:** Change `Search`'s `client Embedder` parameter to `client *Client`.
Delete the `Embedder` declaration. Rewrite `mockEmbedder` as a `*Client` built
against an httptest server (the test already wires one up at
`client_test.go:153`), or convert `Search` to take a function value
(`embedOne func(ctx, text, kind) ([]float32, error)`) if mock-via-fn is
preferred over httptest. Caller impact: 3 call sites, all already pass
`*Client`, so the signature change is local.

**Tier:** borderline. The interface is intra-package and exists purely for
unit-test isolation, which is the lowest-cost variant of this smell — but it
still meets the rule.

---

### 2. [F2] `internal/output/types.go:256` — exported-but-unreferenced

**Diagnosis:** `output.ErrStaleIndex = "STALE_INDEX"` has zero references
anywhere in the repo (prod or test). The neighboring error codes
(`ErrNotFound`, `ErrAmbiguousSymbol`, `ErrMissingIndex`, …) are all used.

**Why:** Dead export. The code-string `"STALE_INDEX"` is produced by
runtime callers via `output.Error{Code: "STALE_INDEX"}`-style construction
elsewhere; the named constant is genuinely unreferenced.

**Evidence:**
- decl: `internal/output/types.go:256`
- `snipe deadcode` reports `refs_all=0`
- `rg 'ErrStaleIndex' --type go -g '!vendor/**'` returns only the declaration line.

**Fix:** Delete the constant. If the intent is to canonicalize the string
so callers reference the constant instead of the literal, do the inverse:
find every literal `"STALE_INDEX"` in cmd/ and replace with `output.ErrStaleIndex`.

**Tier:** action.

---

### 3. [F3] `internal/store/store.go:115` — exported-but-unreferenced

**Diagnosis:** `(*Store).Path()` is exported but only referenced by
same-package tests (`internal/store/store_test.go:20-21`). No external
package, no production code path calls it.

**Why:** The rule's don't-flag clause covers `_test`-suffix external test
packages; this is internal `store_test.go` (same package), not an external
contract. The method exists solely to make a private field readable for a
single assertion.

**Evidence:**
- decl: `internal/store/store.go:115`
- only refs: `internal/store/store_test.go:20`, `internal/store/store_test.go:21`
- `snipe deadcode` reports `refs_all=2`, both test-only same-package.

**Fix:** Unexport to `path()` (same-package test still works). Or, if you
prefer to keep the test reading `s.path` directly, delete the method.

**Tier:** action.

---

### 4. [F4] `internal/util/file.go:130` — exported-but-unreferenced

**Diagnosis:** `(*FileCache).Clear()` is exported but has no callers in prod
or test (only the `// Clear …` comment refs at `store/write.go:58,384` are
unrelated string matches).

**Why:** Dead public method on a cache type. `FileCache` is referenced from
`internal/index` only; nothing calls `Clear()`.

**Evidence:**
- decl: `internal/util/file.go:130`
- `snipe deadcode` reports `refs_all=1` (the comment match, not a real call).
- `rg '\.Clear\(\)' --type go -g '!vendor/**'` returns no `FileCache.Clear` invocations.

**Fix:** Delete the method. Reintroduce when a real flush use case appears.

**Tier:** action.

---

## Not flagged (verified clean)

- **Embedding leaks:** Only one non-vendor embedded type, `query.TraceRef`
  embeds `query.LiteralRef`. `LiteralRef` has zero methods (struct of fields
  only), so no method-set promotion. No `sync.Mutex` embedding outside vendor.
- **Receiver-mix:** All 22 production receiver types are `*T`; only
  `Coupling` uses a value receiver and has a single method — no mix.
- **Value-receive-large-struct:** No exported function signatures take a
  >64-byte non-pointer struct.
- **`Execute` (cmd/root.go:143):** flagged by `snipe deadcode` but called
  from `main.go:6` (standard cobra wiring) — false positive.
- **`SuggestionsForAmbiguous`, `GetEmbedding`:** referenced only by tests,
  but the don't-flag clause for test-package refs applies.
