# snipe

Go code navigation for LLMs. Static indexing, <50ms queries, JSON output.

```bash
go install github.com/dkoosis/snipe@latest
```

## Quick Start

```bash
snipe index                     # Build index (run once, ~5s for most projects)
snipe def ProcessOrder          # Jump to definition
snipe refs ProcessOrder         # Find all references
snipe callers ProcessOrder      # Who calls this?
snipe pack ProcessOrder         # Everything about a symbol in one call
```

## Why snipe?

- **Static indexing** -- No gopls daemon, no LSP. Index once, query in <50ms
- **Semantic search** -- Voyage AI embeddings find symbols by meaning, not just name
- **Pack command** -- Definition + refs + callers + callees + role + purpose in one query
- **Incremental indexing** -- Only re-indexes changed files on subsequent runs
- **Position-first** -- `--at file:line:col` maps directly to compiler error output
- **JSON-first** -- Deterministic `{results, meta, error}` envelope on every command
- **Edit-ready** -- Every result has `edit_target` for direct handoff to editors
- **Hex ID chaining** -- 16-char IDs from one command feed into the next
- **LLM boot context** -- `snipe context --boot` generates project architecture for LLM sessions

## Architecture

```
                          snipe CLI
                             |
            +----------------+----------------+
            |                |                |
       go/packages       ripgrep          SQLite
     (static analysis)  (text search)   (.snipe/index.db)
            |                                 |
            v                                 |
    symbols, refs,                            |
    call graph, types                         |
            |                                 |
            +----------> index <--------------+
                           |
              +------------+------------+
              |                         |
         Voyage AI                 Anthropic API
      (voyage-code-3)           (claude-3-5-haiku)
              |                         |
        embeddings               symbol purposes
       (1024-dim vectors)       (LLM-generated summaries)
              |                         |
              v                         v
           sim search              explain, context
```

**Core pipeline:** `go/packages` extracts symbols, references, and call graph via static analysis. Everything goes into SQLite for fast indexed queries. `ripgrep` handles text search without needing the index.

**Optional enrichment:** With API keys, snipe generates code embeddings (for semantic similarity search) and LLM-generated symbol purposes (for explain and context commands).

## Commands

### Core Commands

| Command | Description |
|---------|-------------|
| `def [symbol]` | Jump to symbol definition |
| `refs [symbol]` | Find all references to a symbol |
| `callers [symbol]` | Find functions that call a symbol |
| `callees [symbol]` | Find functions that a symbol calls |
| `search <pattern>` | Text search via ripgrep (no index needed) |
| `pack [symbol]` | Full symbol profile: def + refs + callers + callees + role + purpose |
| `show <id>` | Expand a symbol by its 16-char hex ID |
| `sym [symbol]` | Combined query (def + refs + callers + callees) |

### Index Commands

| Command | Description |
|---------|-------------|
| `index [path]` | Build or update the code index |
| `status` | Show index status and statistics |
| `doctor` | Check snipe installation and configuration |

### Advanced Commands

| Command | Description |
|---------|-------------|
| `context [path]` | Generate LLM-optimized project context |
| `explain [symbol]` | Structured function explanation |
| `sim <query>` | Semantic similarity search (requires embeddings) |
| `edit [symbol]` | AST-aware code editing |
| `types [type]` | Show type relationships (methods, fields, embeds) |
| `impl [interface]` | Find types implementing an interface |
| `imports <file>` | Show packages imported by a file |
| `importers <pkg>` | Find files that import a package |
| `pkg <name>` | Show package overview with exported symbols |

## Usage Examples

### Definition lookup

```bash
snipe def ProcessOrder           # By name
snipe def --at main.go:42:12     # By position (from error output)
snipe def a3f2c1de89ab0123       # By hex ID (auto-detected)
snipe def handler.go:Server      # File-scoped lookup
```

### Scoped queries

```bash
snipe def --file store.go        # All symbols in a file
snipe def --pkg query            # Exported symbols in a package
snipe refs Open --file store.go  # References filtered to a file
snipe refs Open --pkg internal/query  # References filtered to a package
```

### Hex ID chaining

```bash
# 1. Find the definition
snipe def --at server/handler.go:142:15
# Result includes id: "a3f2c1de89ab0123"

# 2. Who calls it?
snipe callers a3f2c1de89ab0123

# 3. Get full body of a caller
snipe show b4e3d2c1a0f98765 --with-body

# 4. What does that caller call?
snipe callees b4e3d2c1a0f98765
```

### Pack: everything in one call

```bash
snipe pack ProcessOrder              # Full profile by name
snipe pack --at main.go:42:12        # Full profile at position
snipe pack abc123def456 789012345678 # Multiple symbols at once
```

### Indexing

```bash
snipe index                          # Full index with embeddings + enrichment
snipe index --embed-mode=off         # Skip embeddings
snipe index --enrich=false           # Skip LLM enrichment
snipe index --embed-mode=off --enrich=false  # Minimal index (symbols only)
snipe index --force                  # Force full re-index
```

Embedding modes: `auto` (default), `batch` (async), `realtime` (sync), `off`

## Key Flags

| Flag | Effect |
|------|--------|
| `--at file:line:col` | Query by position (from error/compiler output) |
| `--with-body` | Include full function/method body |
| `--limit N` | Cap results (default 50) |
| `--offset N` | Skip first N results (pagination) |
| `--context N` | Lines of context around match (default 3) |
| `--format mode` | `concise`, `detailed`, or `summary` |
| `--select mode` | `all`, `best`, `top3`, `top5` |
| `--max-tokens N` | Truncate output to token budget |
| `--human` | Pretty-print for terminal debugging |
| `--signature-only` | Return only signature (no body, no context) |

## Output Format

All commands return JSON with a consistent envelope:

```json
{
  "protocol": 1,
  "ok": true,
  "results": [{
    "id": "a3f2c1de89ab0123",
    "file": "order/handler.go",
    "range": {"start": {"line": 42, "col": 4}, "end": {"line": 42, "col": 31}},
    "kind": "func",
    "name": "ProcessOrder",
    "match": "func ProcessOrder(ctx context.Context, order *Order) error",
    "edit_target": "order/handler.go:42:4-42:31#abc123",
    "role": "handler"
  }],
  "suggestions": ["snipe refs ProcessOrder", "snipe callers ProcessOrder"],
  "meta": {
    "command": "def",
    "ms": 23,
    "total": 1,
    "index_state": "fresh",
    "token_estimate": 450,
    "stale_files": []
  }
}
```

Key fields:
- `id` -- 16-char hex, usable in subsequent commands
- `edit_target` -- direct handoff format for editors
- `role` -- architectural classification (handler, model, util, etc.)
- `meta.index_state` -- `fresh`, `stale`, or `missing`
- `meta.stale_files` -- files modified since last index
- `suggestions` -- recommended next commands

## Embeddings: Why voyage-code-3

snipe uses [Voyage AI's voyage-code-3](https://docs.voyageai.com/docs/embeddings) for semantic search:

- **Code-specialized** -- Trained on code corpora, understands Go identifiers, signatures, and patterns better than general-purpose embedding models
- **1024-dimensional vectors** -- Good balance of quality vs storage for per-symbol embeddings
- **Batch + realtime APIs** -- Batch API (async) for initial large indexes, realtime API for incremental updates
- **Configurable** -- Override with `VOYAGE_MODEL` env var if you prefer a different model

Embeddings power `snipe sim` (semantic similarity search) and enhance `snipe context` output.

## Configuration

### Environment variables

| Variable | Purpose |
|----------|---------|
| `VOYAGE_API_KEY` | Enable embeddings (Voyage AI) |
| `VOYAGE_MODEL` | Override embedding model (default: voyage-code-3) |
| `VOYAGE_API_URL` | Override Voyage API endpoint |
| `ANTHROPIC_API_KEY` | Enable LLM enrichment (symbol purposes) |

### Project config

Create `.snipe.json` in your project root:

```json
{
  "limit": 100,
  "context_lines": 5,
  "excludes": ["vendor/**", "testdata/**"]
}
```

Or global config at `~/.config/snipe/config.json` (project config overrides global).

## Requirements

- Go 1.21+ (for `go/packages` analysis)
- [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) for text search
- SQLite (bundled via modernc.org/sqlite, no CGO needed)

Run `snipe doctor` to verify your setup.
