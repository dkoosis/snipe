# io-parallel — snipe repo review

run_id: ecebe5258308
scope: project

## I/O surface gate

snipe is a local CLI. The full network/RPC surface is:

- **Voyage AI embeddings API** (HTTPS): one client (`internal/embed/client.go`) for realtime `/embeddings`, one BatchClient (`internal/embed/batch.go`) for `/files`, `/batches`, batch status, file download.
- **SQLite** at `.snipe/index.db`: every other "I/O" call.

Per the io-parallel rules (`sequential-db-reads`): "For SQLite, parallelism may not help — note backend specifics." SQLite holds a single writer mutex and parallel reads share one file; goroutine fanout against `*sql.DB` here would add coordination cost without latency win. The Go-AST extract pipeline (`ExtractSymbols` → `ExtractRefs` → `ExtractCallGraph` → `ExtractImports` → `ExtractFileInfo`) is CPU-bound, not I/O, and outside this linter's scope.

The only candidates for real parallel-I/O wins are the Voyage call sites. There is no errgroup or `golang.org/x/sync` usage anywhere in `internal/` or `cmd/` (the vendor hits are transitive through `golang.org/x/tools/go/packages`). One `sync.WaitGroup` exists, in a store test.

## Findings

### F1 [P1 loop-independent-io] `generateEmbeddings` — sequential Voyage batches

**Function:** `cmd.generateEmbeddings` — `/Users/vcto/Projects/snipe/cmd/index.go:365`

**Independent calls:** N batches of `client.Embed(ctx, texts, "document")` against the Voyage `/embeddings` endpoint, one per chunk of 64 symbols. Each call's input (`texts`) depends only on the slice window `[i:i+64]`; no batch consumes a prior batch's result. The only cross-iteration state is `total++` and `s.SaveEmbedding(...)`, both commutative.

**Latency win:** sum vs max. For a repo with ~6000 embeddable symbols at batch=64 = ~94 sequential HTTPS round-trips at typical 300-800ms each → 30-75s, vs ~max(latency) with bounded fanout. Realistic win at `errgroup.SetLimit(8)` ≈ 8× on the embed phase. This is the dominant wall-clock cost of `snipe index` on a fresh repo when realtime mode is selected.

**Code (cmd/index.go:382-413):**

```go
for i := 0; i < len(toEmbed); i += batchSize {
    end := i + batchSize
    if end > len(toEmbed) {
        end = len(toEmbed)
    }
    batch := toEmbed[i:end]
    texts := make([]string, len(batch))
    for j, sym := range batch {
        texts[j] = sym.Text
    }
    embeddings, err := client.Embed(ctx, texts, "document")  // sequential RPC
    if err != nil {
        return total, fmt.Errorf("embed batch %d: %w", i/batchSize, err)
    }
    for j, emb := range embeddings {
        if emb == nil {
            continue
        }
        if err := s.SaveEmbedding(batch[j].ID, emb, client.Model()); err != nil {
            return total, fmt.Errorf("save embedding for %s: %w", batch[j].ID, err)
        }
        total++
    }
    fmt.Fprintf(os.Stderr, "  Embedded %d/%d symbols\n", end, len(toEmbed))
}
```

**Fix:** errgroup with bounded fanout. `SetLimit` keeps concurrent request count under Voyage's rate limit; SaveEmbedding is the single-writer SQLite path so guard with a mutex or channel-serialize the writes.

```go
g, gctx := errgroup.WithContext(ctx)
g.SetLimit(8) // tune vs Voyage rate limit / observed 429s
var mu sync.Mutex // SQLite single-writer
var total atomic.Int64

for i := 0; i < len(toEmbed); i += batchSize {
    end := min(i+batchSize, len(toEmbed))
    batch := toEmbed[i:end]
    g.Go(func() error {
        texts := make([]string, len(batch))
        for j, sym := range batch {
            texts[j] = sym.Text
        }
        embeddings, err := client.Embed(gctx, texts, "document")
        if err != nil {
            return fmt.Errorf("embed batch starting at %d: %w", i, err)
        }
        mu.Lock()
        defer mu.Unlock()
        for j, emb := range embeddings {
            if emb == nil {
                continue
            }
            if err := s.SaveEmbedding(batch[j].ID, emb, client.Model()); err != nil {
                return fmt.Errorf("save embedding for %s: %w", batch[j].ID, err)
            }
            total.Add(1)
        }
        return nil
    })
}
if err := g.Wait(); err != nil {
    return int(total.Load()), err
}
```

Caveat to verify before shipping: confirm Voyage AI's per-key concurrency limit, then set `SetLimit` accordingly. The progress log loses its monotonic "i/N" shape — replace with `atomic.Int64` running counter if the stderr trail matters.

## Considered and rejected

- **`cmd.runIndex` extract pipeline** (`cmd/index.go:178-225`) — ExtractSymbols → ExtractRefs → ExtractCallGraph → ExtractImports → ExtractFileInfo. The latter four are mutually independent given `result` and `symbols`, but they are CPU-bound AST traversals over an in-memory `*packages.Package` graph, not I/O. Out of scope for io-parallel; would belong to a CPU-fanout review.
- **`internal/context.GenerateBoot`** (`internal/context/generate.go:53-128`) — issues 14+ sequential `db.Query` / `db.QueryRow` calls against SQLite. Independent in data-flow, but per rule `sequential-db-reads` SQLite parallelism is not a latency win; the bottleneck is the single file and the cgo boundary, not round-trips.
- **`startBatchEmbeddings`** (`cmd/index.go:456`) — Upload → CreateBatch → SaveState is a true serial chain (each consumes the previous result's ID).
- **`runEmbedStatus`** (`cmd/embed.go:65`) — single `GetBatchStatus` then conditional `DownloadFile`; dependent.

## Summary

| Tier | Count |
|------|-------|
| 🔴 | 0 |
| 🟡 | 1 |
| 🟢 | — |

One actionable finding. The only real network-I/O surface in snipe is Voyage AI, and `generateEmbeddings` is the only place that issues independent calls in series. Everything else is SQLite (rule-exempted) or CPU-bound AST work.

trixi log-skill io-parallel findings 1 --run-id "ecebe5258308"
