# snipe: Code Navigation CLI for LLMs

## tl;dr

Single Go binary for fast, JSON-first code navigation. Optimized for Claude. Static indexing (no gopls daemon). Will spin out to separate repo.

## Problem

AI wastes tokens on exploratory navigation:
- rg finds text, not symbols
- gopls has cold-start latency, requires daemon
- Results lack context for immediate editing
- No standard nav → edit handoff format

## Solution

```
snipe index                    # Build SQLite index via go/packages
snipe def --at file:line:col   # Definition at position
snipe refs ProcessOrder        # All references
snipe callers ProcessOrder     # Call sites (static)
snipe show --id abc123         # Expand previous result
```

**Key design decisions:**
- **Static indexer** via go/packages, not gopls — no daemon, <50ms queries
- **Position-first** addressing (`--at file:line:col`) — what Claude actually has
- **Determinism contract** — all commands return `{results, meta, error}`
- **Edit-ready output** — `edit_target` field for direct handoff
- **Graceful degradation** — core works without embeddings

## Architecture

```
┌─────────────────────────────────────┐
│            snipe CLI                │
├───────────┬───────────┬─────────────┤
│    rg     │go/packages│  embeddings │
│  (shell)  │ (compiled)│ (optional)  │
└───────────┴─────┬─────┴─────────────┘
                  ▼
        ┌─────────────────┐
        │ SQLite (indexed)│
        │ symbols, refs,  │
        │ call_graph      │
        └─────────────────┘
```

## SQLite durability and maintenance notes

The index database is opened with WAL and `synchronous=NORMAL` to balance
durability and latency for short-lived indexing and query workloads. If you
need maximum durability (e.g., running in environments with frequent power
loss), consider profiling with `synchronous=FULL` before changing defaults,
as it can materially increase write latency. Use `ANALYZE` after large schema
changes or bulk reindexing to refresh SQLite statistics. Periodic `VACUUM`
is only recommended if you observe unbounded database growth after deletes,
since it rewrites the entire file and can be I/O intensive.

## Output Example

```json
{
  "results": [{
    "id": "a3f2c1",
    "file": "order/handler.go",
    "range": { "start": {"line": 42, "col": 4}, "end": {"line": 42, "col": 31} },
    "match": "ProcessOrder(ctx, req.Order)",
    "enclosing": {
      "kind": "func",
      "name": "HandleCheckout",
      "signature": "func HandleCheckout(ctx context.Context, req *Request) error"
    },
    "edit_target": "order/handler.go:42:4-42:31"
  }],
  "meta": { "ms": 23, "total": 7, "index_state": "fresh", "token_estimate": 450 }
}
```

## Phases

1. **MVP**: index, search, def, refs (Go only)
2. **Complete**: callers, callees, impl, type, show
3. **Semantic**: `snipe sim` with embeddings
4. **Multi-lang**: TS/JS via tsserver

## Spin-out Plan

- [ ] Create `snipe` repo
- [ ] Move spec
- [ ] Phase 1 implementation
- [ ] Orca integration (MCP wrapper or direct CLI use)

---

Full spec attached below.
