# snipe: Code Navigation CLI for LLMs

## Quick Start

```bash
snipe index                     # Build index (run once, rerun when code changes)
snipe def ProcessOrder          # Jump to definition by name
snipe def --at main.go:42:12    # Jump to definition at position
snipe refs ProcessOrder         # Find all references
snipe callers ProcessOrder      # Who calls this?
snipe callees ProcessOrder      # What does this call?
snipe show a3f2c1de89ab0123     # Expand result by hex ID
```

## Why snipe?

- **Static indexing**: No gopls daemon, <50ms queries
- **Position-first**: `--at file:line:col` maps directly to error output
- **JSON-first**: Deterministic `{results, meta, error}` format
- **Edit-ready**: Every result has `edit_target` for direct handoff
- **Hex ID chaining**: Use IDs from one command in another

## Commands

### `snipe index`
Build SQLite index from Go source. Run before other commands.

By default, generates embeddings and LLM-based symbol purposes for enhanced context.
```bash
snipe index              # Index with embeddings + enrichment (default)
snipe index /path/to/repo  # Index a different directory
snipe index --enrich=false # Skip LLM enrichment
snipe index --embed-mode=off  # Skip embeddings
snipe index --embed-mode=off --enrich=false  # Minimal index (symbols only)
```

Embedding modes:
- `auto` (default): Batch API for initial index, realtime for incremental
- `batch`: Force async batch API (up to 12h completion)
- `realtime`: Force sync API (may timeout on large codebases)
- `off`: Skip embeddings

### `snipe def [symbol|id]`
Jump to symbol definition.
```bash
snipe def ProcessOrder           # By name
snipe def --at main.go:42:12     # By position (from error output)
snipe def a3f2c1de89ab0123       # By hex ID (auto-detected)
snipe def handler.go:Server      # File-scoped lookup
snipe def --with-body ProcessOrder  # Include function body
```

### `snipe refs [symbol]`
Find all references to a symbol.
```bash
snipe refs ProcessOrder          # All references
snipe refs --at main.go:42:12    # References to symbol at position
snipe refs Workspace --kind=method  # Filter by enclosing kind
```

### `snipe callers [symbol|id]`
Find functions that call a symbol.
```bash
snipe callers ProcessOrder       # Who calls ProcessOrder?
snipe callers a3f2c1de89ab0123   # By hex ID (auto-detected)
snipe callers --with-body ProcessOrder  # Include caller bodies
```

### `snipe callees [symbol|id]`
Find functions that a symbol calls.
```bash
snipe callees ProcessOrder       # What does ProcessOrder call?
snipe callees a3f2c1de89ab0123   # By hex ID (auto-detected)
snipe callees --with-body ProcessOrder  # Include callee bodies
```

### `snipe show <id>`
Expand a hex ID from previous results.
```bash
snipe show a3f2c1de89ab0123      # Get full details for ID
snipe show a3f2c1de89ab0123 --with-body  # Include body
```

### `snipe search <pattern>`
Text search via ripgrep. Works without an index.
```bash
snipe search "TODO:"             # Find all TODOs
snipe search "func.*Handler"     # Regex search
```

### `snipe doctor`
Check index health and freshness.
```bash
snipe doctor                     # Quick health check
```

## Key Flags

| Flag | Effect |
|------|--------|
| `--at file:line:col` | Query by position (from error/output) |
| `--with-body` | Include full function/method body |
| `--limit N` | Cap results (default 50) |
| `--offset N` | Skip first N results (pagination) |
| `--context N` | Lines of context around match (default 3) |
| `--summary` | Return counts per file instead of full results |
| `--max-tokens N` | Truncate output to token budget |
| `--human` | Pretty-print for debugging |

## Output Format

All commands return JSON:
```json
{
  "results": [{
    "id": "a3f2c1de89ab0123",
    "file": "order/handler.go",
    "range": {"start": {"line": 42, "col": 4}, "end": {"line": 42, "col": 31}},
    "kind": "func",
    "name": "ProcessOrder",
    "match": "func ProcessOrder(ctx context.Context, order *Order) error",
    "edit_target": "order/handler.go:42:4-42:31#abc123"
  }],
  "meta": {
    "ms": 23,
    "total": 7,
    "index_state": "fresh",
    "token_estimate": 450
  }
}
```

Key fields:
- `id`: 16-char hex ID, usable in subsequent commands
- `edit_target`: Direct handoff format for edits
- `index_state`: "fresh", "stale", or "missing"

## Workflow Example

```bash
# 1. Index the codebase
snipe index

# 2. Find definition of a function mentioned in error
snipe def --at server/handler.go:142:15

# 3. Result shows ID a3f2c1de89ab0123. Find who calls it:
snipe callers a3f2c1de89ab0123

# 4. Get full body of a caller:
snipe show b4e3d2c1a0f98765 --with-body

# 5. What does that caller call?
snipe callees b4e3d2c1a0f98765
```

## Architecture

```
snipe CLI
    |
    +-- go/packages (static indexing)
    +-- ripgrep (text search)
    +-- Voyage AI (embeddings)
    +-- Anthropic API (LLM enrichment)
    |
    v
SQLite (.snipe/index.db)
    +-- symbols (id, name, kind, file, range, signature, doc)
    +-- refs (symbol_id, file, line, col, enclosing_id)
    +-- call_graph (caller_id, callee_id, file, line, col)
    +-- embeddings (semantic vectors for similarity search)
    +-- symbol_purposes (LLM-generated descriptions with content hashes)
```

## Configuration

Create `.snipe/config.json`:
```json
{
  "limit": 100,
  "context_lines": 5,
  "embed_mode": "auto",
  "enrich": true
}
```

Environment variables:
- `VOYAGE_API_KEY`: Enable embeddings (batch/realtime modes)
- `ANTHROPIC_API_KEY`: Enable LLM enrichment (symbol purposes)
