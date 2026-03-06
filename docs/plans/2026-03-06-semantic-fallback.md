# Plan: Semantic fallback in `snipe search`

## Context

Claude's #1 pain: "find the thing that does X" when it doesn't know the symbol name. `snipe search` is ripgrep-only. `snipe sim` exists but is a separate command nobody invokes. Wire sim as automatic fallback when rg returns zero results.

## Approach

When `snipe search` gets 0 ripgrep results AND embeddings are available, automatically run the sim pipeline and return those results. Signal the mode switch via `Meta.DecisionPath`.

**Not** merging rg + sim results together — only fallback on zero results. This avoids score normalization problems and keeps behavior predictable.

## Files to modify

1. **`internal/embed/search.go`** (NEW) — extracted semantic search core, testable without `cmd/`
2. **`cmd/sim.go`** — refactor `runSim` to call the extracted core
3. **`cmd/search.go`** — add fallback logic after rg returns empty
4. **`internal/output/types.go`** — update `SuggestionsForSearch` to accept fallback state

## Files to read (no changes)

- `internal/embed/client.go` — `NewClient()`, `EmbedOne()`, `HasCredentials()`
- `internal/store/embed.go` — `GetAllEmbeddings()`, `EmbeddingRow`, `CountEmbeddings()`
- `internal/query/lookup.go` — `LookupByID()`
- `internal/output/types.go` — `Response`, `Result`, `Meta.DecisionPath`
- `cmd/search.go` — existing store-open pattern for enrichment (lines 84-100)

## Implementation

### Step 1: Extract sim core into `internal/embed/search.go`

The reusable logic lives in `internal/`, not `cmd/`, per project layout convention.

```go
// internal/embed/search.go
package embed

import (
	"database/sql"
	"sort"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

// SemanticResult pairs a symbol result with its similarity score.
type SemanticResult struct {
	Result     output.Result
	Similarity float32
}

// SemanticSearch embeds the query, compares against all stored embeddings,
// and returns results above the threshold sorted by similarity descending.
// Returns (nil, nil) if no embeddings exist — caller decides how to handle.
func SemanticSearch(queryText string, s *store.Store, client *Client, limit int, threshold float32) ([]output.Result, error) {
	count, err := s.CountEmbeddings()
	if err != nil || count == 0 {
		return nil, nil // no embeddings — not an error, just nothing to search
	}

	queryEmbed, err := client.EmbedOne(queryText, "query")
	if err != nil {
		return nil, err
	}

	embeddings, err := s.GetAllEmbeddings()
	if err != nil {
		return nil, err
	}

	var matches []SemanticResult
	for _, e := range embeddings {
		sim := CosineSimilarity(queryEmbed, e.Embedding)
		if sim >= threshold {
			matches = append(matches, SemanticResult{
				Similarity: sim,
			})
			// Defer symbol lookup to after filtering
			matches[len(matches)-1].Result.ID = e.SymbolID
			matches[len(matches)-1].Result.Score = float64(sim)
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

	// Hydrate results with full symbol info
	results := make([]output.Result, 0, len(matches))
	for _, m := range matches {
		sym, err := query.LookupByID(s.DB(), m.Result.ID)
		if err != nil || sym == nil {
			continue // skip unresolvable symbols
		}
		result := sym.ToResult()
		result.Score = float64(m.Similarity)
		results = append(results, result)
	}

	return results, nil
}
```

### Step 2: Write tests for `SemanticSearch`

```go
// internal/embed/search_test.go
```

Test cases:
- Returns nil, nil when no embeddings exist (CountEmbeddings == 0)
- Returns results above threshold, sorted by similarity descending
- Respects limit parameter
- Skips unresolvable symbol IDs gracefully

These need a test double for `Client.EmbedOne` — either an interface or a constructor that accepts a custom HTTP transport. Evaluate which is simpler; a `Searcher` interface with a mock is likely cleanest since `EmbedOne` makes a network call.

### Step 3: Refactor `cmd/sim.go` to call extracted core

Replace lines 99-130 of `runSim` with a call to `embed.SemanticSearch(queryText, s, client, lim, threshold)`. `runSim` retains: flag parsing, store opening, output formatting, token budget, offset/limit, summary mode.

Run `mage` — sim behavior must be identical.

### Step 4: Add fallback in `cmd/search.go`

**Critical: store handle lifecycle.** Currently, the store is opened inside an `if` block (line 85) and closed implicitly when the block exits scope. For fallback, we need the store handle to survive. Hoist the store open to before the rg call:

```go
// Open store once — used for both enrichment and potential fallback
dbPath := store.DefaultIndexPath(dir)
var s *store.Store
if store.Exists(dbPath) && !store.IsIndexing(dbPath) {
    if opened, err := store.Open(dbPath); err == nil {
        s = opened
        defer s.Close()
    }
}
```

After rg search returns, if `len(results) == 0` AND `err == nil`:

1. **Guard: skip if `--file` is set** — user is doing a targeted search, semantic fallback would be surprising
2. **Guard: skip if no credentials** — `embed.HasCredentials()`, silent skip
3. **Guard: skip if no store or no embeddings** — `s == nil`, silent skip
4. Call `embed.SemanticSearch(pattern, s, client, lim, 0.3)`
5. If results found: use them, set `DecisionPath = ["rg:0_results", "sim:N_results"]`
6. If no results or error: proceed normally, add `"semantic_fallback_attempted"` to DecisionPath

```go
// Semantic fallback — only on zero rg results, no error, no --file filter
if len(results) == 0 && searchFile == "" && s != nil && embed.HasCredentials() {
    client, err := embed.NewClient()
    if err == nil {
        simResults, simErr := embed.SemanticSearch(pattern, s, client, lim, 0.3)
        if simErr == nil && len(simResults) > 0 {
            results = simResults
            decisionPath = []string{
                "rg:0_results",
                fmt.Sprintf("sim:%d_results", len(simResults)),
            }
            usedFallback = true
        }
    }
    // Errors silently ignored — fallback is best-effort
}
```

### Step 5: Update suggestions for fallback state

Modify `SuggestionsForSearch` signature to accept fallback info:

```go
func SuggestionsForSearch(pattern string, resultCount int, usedFallback bool) []Suggestion
```

When `usedFallback && resultCount > 0`:
- Suggest `snipe sim "<pattern>"` — "Results from semantic similarity — use 'snipe sim' for more control"
- Drop the `--context 5` suggestion (irrelevant for semantic results)

When `!usedFallback && resultCount == 0`:
- Keep existing suggestions
- Add `snipe sim "<pattern>"` — "Try semantic search if you're looking for concepts, not exact text"

### Step 6: Update search command help text

Add to `searchCmd.Long`:

```
If no text matches are found and embeddings are available, automatically
falls back to semantic similarity search. Use 'snipe sim' directly for
more control over semantic search parameters.
```

### Step 7: Blackbox test

Add to `test/blackbox/` following existing patterns:

1. **Fallback triggers:** Index a fixture with embeddings, search for a term that has no literal match but semantic match → verify `decision_path` contains `"sim:..."`, results non-empty
2. **Fallback suppressed by `--file`:** Same fixture, add `--file "*.go"` → verify no fallback, empty results
3. **Fallback suppressed by no embeddings:** Index without embeddings (`--embed-mode=off`), search → verify no fallback, empty results
4. **rg results take precedence:** Search for a term that has literal matches → verify no `decision_path` entry for sim

### Step 8: Commit sequence

```
refactor: extract SemanticSearch into internal/embed/search.go
refactor: wire cmd/sim.go to use extracted SemanticSearch
feat: add semantic fallback to snipe search (#fallback)
test: add blackbox tests for semantic fallback
```

## Constraints

- **No fallback if no credentials** — silent skip, no error
- **No fallback if `--file` is set** — targeted search should stay targeted
- **Only on zero rg results with no error** — don't mix result sources, don't mask rg failures
- **sim threshold 0.3** — hardcoded default, not a new flag
- **No new flags on search** — keep it simple
- **Fallback latency:** `GetAllEmbeddings()` is O(n) brute force. Log sim latency in DecisionPath (e.g., `"sim:5_results:23ms"`). If this exceeds 50ms on real repos, file a follow-up for ANN indexing — don't block this feature on it.

## Verification

1. `mage` passes after each commit (build + lint + test)
2. `snipe sim "cosine similarity"` — behavior unchanged after refactor (step 3)
3. `snipe search "nonexistent_garbage"` — returns empty, no fallback (no semantic match either)
4. `snipe search "handle HTTP request"` — falls back to sim if no literal match, DecisionPath shows it
5. `snipe search "TODO" --file "*.go"` — no fallback even if 0 results
6. `VOYAGE_API_KEY="" snipe search "validate input"` — gracefully skips fallback
7. Blackbox tests pass
8. `mage qa` passes

## Open questions

- Should DecisionPath include fallback latency? (Recommended yes — helps diagnose slow queries)
- `SemanticSearch` currently does `LookupByID` per result (N+1). Batch lookup exists (`BatchLookupByID`). Use it if N > ~5. Low priority for initial impl since results are already capped by `limit`.
