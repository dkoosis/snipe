# zero-sentinel — repo

run-id: ecebe5258308
scope: project (snipe)
date: 2026-05-17

## Summary

Snipe is mostly insulated from the canonical zero-sentinel hazards: it has no SQL persistence of `time.Time` fields (SQLite store holds only blobs, paths, and ints), no `uuid.UUID` usage outside vendored deps, and almost every map read is comma-ok-guarded or post-populated. The remaining hazards cluster around one persisted JSON struct (`embed.BatchState`) where `time.Time` fields are used both as wall-clock sources for `time.Since` staleness checks and as JSON round-trip values — a zero value on a legacy or hand-edited state file would silently mis-trip recovery logic.

| Tier | Count | Verdict |
|------|-------|---------|
| P1 time | 2 | 🟡 |
| P1 ambiguity | 1 | 🟡 |
| P1 map-index | 1 | 🟢 (non-critical) |
| P2 boundary | 1 | 🟡 |

Overall: 🟡

## Findings

### 1. `state.UpdatedAt` zero → infinite staleness on legacy/corrupt state

- **Site:** `cmd/index.go:485`
- **Zero value:** `time.Time{}`
- **Domain meaning conflated:** "never updated" vs "field absent in old state file"
- **Failure mode:** `time.Since(state.UpdatedAt)` on a zero value returns ~2000 years. The staleness threshold check always trips, and the recovery branch at L487-509 clears the batch state and pays for a re-embed. Triggers when an older snipe writes a `batch_state.json` without `UpdatedAt`, or when a user edits the file and the field round-trips to zero. `LoadState` (`internal/embed/batch.go:399`) Unmarshals partial JSON cleanly — no validation rejects a zero timestamp.

```go
if state != nil && (state.Status == "validating" || state.Status == "in_progress") {
    age := time.Since(state.UpdatedAt)       // ← zero → ~17,500,000h
    switch {
    case age > batchStaleThreshold:
        // ... reach Voyage, possibly clear state, possibly burn a billed batch
```

- **Fix:** In `LoadState`, treat `state.UpdatedAt.IsZero()` as "unknown → assume stale just-now" (set to `time.Now()`), or change `UpdatedAt time.Time` → `*time.Time` and require non-nil before age comparisons.

---

### 2. `state.CreatedAt` zero → bogus age string in status output

- **Site:** `cmd/embed.go:185`
- **Zero value:** `time.Time{}`
- **Domain meaning conflated:** "creation time unknown" vs "created at 0001-01-01"
- **Failure mode:** `time.Since(state.CreatedAt)` on a legacy state surfaces as `"17532000h0m0s"` in `EmbedStatusResult.Age`. Cosmetic in this case (not on a write path), but the same `state.CreatedAt` is emitted as JSON in three `EmbedStatusResult` constructors (L147, L172, L198) — downstream consumers that key off `created_at` see `"0001-01-01T00:00:00Z"`.

```go
age := time.Since(state.CreatedAt)
// ...
Age: age.Round(time.Minute).String(),
```

- **Fix:** Guard with `if !state.CreatedAt.IsZero()` before computing age; omit the field from JSON output when zero (add `omitempty` to `BatchState.CreatedAt`/`UpdatedAt` JSON tags and use pointer types).

---

### 3. `BatchState.CreatedAt`/`UpdatedAt` — value-typed optionals with no IsZero contract

- **Site:** `internal/embed/batch.go:39-40`
- **Zero value:** `time.Time{}`
- **Domain meaning conflated:** these timestamps are required for staleness/age logic, but the type allows absence (via JSON omission or zero-value construction) and no constructor enforces non-zero
- **Failure mode:** The struct round-trips through JSON (`SaveState`/`LoadState`); any caller that produces a `BatchState{}` literal — e.g. a future migration helper, a debug dump replayed into Load — yields silent zero timestamps that propagate to (1) and (2).

```go
type BatchState struct {
    // ...
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
    // IndexFingerprint correctly uses ",omitempty" + MatchesFingerprint guard;
    // CreatedAt/UpdatedAt have neither.
```

- **Fix:** Either (a) pair each with a companion `*time.Time` and treat nil as "unknown", or (b) add a `(s *BatchState) Validate() error` that rejects zero timestamps in `LoadState` so corruption is loud, matching the existing `MatchesFingerprint("") == false` defensive pattern.

---

### 4. Boundary roundtrip — `BatchState` JSON unmarshal accepts zero timestamps silently

- **Site:** `internal/embed/batch.go:399-406`
- **Zero value:** `time.Time{}` after `json.Unmarshal` with missing field
- **Domain meaning conflated:** marshal/unmarshal symmetry breaks — a state written by an older binary (pre-`UpdatedAt`) loads with zero, then `time.Since` treats it as ancient
- **Failure mode:** Forward-compat with older state files is a stated goal (note the `MatchesFingerprint` doc-comment: "Returns false if either fingerprint is empty (legacy state or missing fingerprint)"). The same legacy-state path is unprotected for timestamps. This is the canonical "encode emits zero for absent; decode reads zero back as zero" pattern from the rules.

```go
var state BatchState
if err := json.Unmarshal(data, &state); err != nil { /* discard */ }
// state.UpdatedAt may be zero if field absent in `data`
return &state, nil
```

- **Fix:** After Unmarshal, normalize zero timestamps to a sentinel (e.g. set both to `time.Now()` with a stderr warning), or upgrade fields to pointer types and refuse to use them when nil.

---

### 5. `coupling.go` Ca counter on edges to nodes outside the universe

- **Site:** `internal/graphmetrics/coupling.go:31`
- **Zero value:** `Coupling{}` from missing map key
- **Domain meaning conflated:** "node tracked in the metrics set" vs "edge destination outside the set"
- **Failure mode:** `out` is pre-seeded only from `nodes` (L25-28). The loop `for _, d := range dsts { c := out[d]; c.Ca++; out[d] = c }` will silently create a fresh `Coupling{Ce:0, Ca:1}` entry for any `d` not in `nodes`. If `nodes` ever filters (e.g. by visibility or package), this introduces phantom nodes with Ce=0 into the output, distorting downstream rankings. Currently `nodes` includes every key in `g.Adj`, so this is benign today — but the invariant is implicit, not enforced.

```go
out := make(map[string]Coupling, len(nodes))
for _, n := range nodes { out[n] = Coupling{Ce: len(g.Adj[n])} }
for _, dsts := range g.Adj {
    for _, d := range dsts {
        c := out[d]   // ← zero-default if d ∉ nodes
        c.Ca++
        out[d] = c
    }
}
```

- **Fix:** Guard with `if _, ok := out[d]; !ok { continue }` (or document that `nodes ⊇ ∪ dsts` is a precondition and assert it at the top of the function).

## Not flagged (intentional zero semantics)

- `internal/lifecycle/classify.go:87-99` — `byEnc[""]` as file-scope bucket is documented at L85-86. Empty-string is the typed sentinel for the domain.
- `internal/util/file.go:115` — `var oldestTime time.Time` LRU eviction; first-iteration guard via `oldestPath == ""` is correct.
- `cmd/*.go` `start time.Time` parameters — all populated by `time.Now()` at command entry, never persisted, used only for `time.Since` in the same call frame.
- `internal/embed/batch.go:47` `MatchesFingerprint` — already implements the desired guard pattern for the string-as-absent case.
- `EnclosingID == ""` for signature-line refs — covered by the existing `reattachSignatureRefs` trap; this is recovery code, not a sentinel bug.

## Files touched (read-only)

- `/Users/vcto/Projects/snipe/internal/embed/batch.go`
- `/Users/vcto/Projects/snipe/cmd/embed.go`
- `/Users/vcto/Projects/snipe/cmd/index.go`
- `/Users/vcto/Projects/snipe/internal/util/file.go`
- `/Users/vcto/Projects/snipe/internal/lifecycle/classify.go`
- `/Users/vcto/Projects/snipe/internal/graphmetrics/coupling.go`
- `/Users/vcto/Projects/snipe/internal/store/embed.go`
- `/Users/vcto/Projects/snipe/internal/query/state.go`
