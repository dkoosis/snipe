# snipe: Code Navigation CLI for LLMs

## Summary

Single Go binary providing fast, deterministic code navigation optimized for LLM consumption. JSON-first output, position-addressed queries, static indexing for speed.

## Problem

AI coding assistants waste tokens on exploratory navigation:
- Text search (rg) lacks semantic awareness
- LSP tools require exact symbol names and have cold-start latency
- Results lack context needed for immediate editing action
- No standard handoff format between navigation and editing tools

## Target User

Claude (and other LLMs). Human-usable but optimized for:
- Token efficiency
- Deterministic, parseable output
- Pipeable to edit tools

## Non-goals

- Editing (pipe to other tools)
- IDE features (diagnostics, formatting, completion)
- Real-time / watch mode
- Multi-repo search

---

## Architecture

```
┌─────────────────────────────────────┐
│            snipe CLI                │
├───────────┬───────────┬─────────────┤
│    rg     │go/packages│  embeddings │
│  (shell)  │ go/types  │ (optional)  │
└─────┬─────┴─────┬─────┴──────┬──────┘
      │           │            │
      └───────────┴─────┬──────┘
                        ▼
              ┌─────────────────────────┐
              │    SQLite index         │
              │  symbols | refs | calls │
              └─────────────────────────┘
```

### Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| No gopls dependency | Static indexing via go/packages | No daemon, no install friction, <50ms queries |
| SQLite index | Precomputed symbols, refs, call graph | SQL JOINs beat runtime analysis |
| rg for text search | Shell out | Proven, fast, no need to reimplement |
| Embeddings optional | Pluggable providers | Core AST nav works with zero config |

### Index Schema

```sql
CREATE TABLE symbols (
    id TEXT PRIMARY KEY,  -- hash of file:range:kind
    name TEXT NOT NULL,
    kind TEXT NOT NULL,   -- func, type, interface, var, const
    file_path TEXT NOT NULL,
    line_start INT NOT NULL,
    col_start INT NOT NULL,
    line_end INT NOT NULL,
    col_end INT NOT NULL,
    signature TEXT,       -- "func Foo(a int) error"
    doc TEXT,             -- doc comment
    receiver TEXT         -- for methods: "(*T)" or "(T)"
);

CREATE TABLE refs (
    id TEXT PRIMARY KEY,
    symbol_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    line INT NOT NULL,
    col INT NOT NULL,
    enclosing_id TEXT,    -- containing function/method
    snippet TEXT,         -- the line of code
    FOREIGN KEY (symbol_id) REFERENCES symbols(id),
    FOREIGN KEY (enclosing_id) REFERENCES symbols(id)
);

CREATE TABLE call_graph (
    caller_id TEXT NOT NULL,
    callee_id TEXT NOT NULL,
    file_path TEXT NOT NULL,
    line INT NOT NULL,
    col INT NOT NULL,
    PRIMARY KEY (caller_id, callee_id, line, col),
    FOREIGN KEY (caller_id) REFERENCES symbols(id),
    FOREIGN KEY (callee_id) REFERENCES symbols(id)
);

CREATE TABLE meta (
    key TEXT PRIMARY KEY,
    value TEXT
);
-- keys: version, repo_root, go_version, indexed_at, fingerprint
```

### Index Invalidation

Fingerprint includes:
- snipe version
- hash of go.mod, go.sum, go.work (if present)
- relevant go env: GOOS, GOARCH, GOMOD, GOWORK

Staleness detection:
- Per-file mtime tracking
- Fingerprint change → full reindex
- File mtime change → incremental update

---

## Commands

### Core Navigation

```
snipe index [path]              # Build/update index
snipe search <pattern>          # Text search via rg
snipe def <symbol>              # Jump to definition
snipe def --at file:line:col    # Definition of symbol at position
snipe refs <symbol>             # Find all references
snipe refs --at file:line:col   # References to symbol at position
snipe callers <symbol>          # Incoming calls (static)
snipe callees <symbol>          # Outgoing calls
snipe impl <interface>          # Types implementing interface
snipe type --at file:line:col   # Type of expression at position
```

### Context & Show

```
snipe show <id>                 # Expand previous result by ID
snipe show --at file:line       # Show enclosing context at position
snipe show --at file:line --full # Include full function body
```

### Semantic Search (Phase 3)

```
snipe sim --at file:line        # Find similar code
snipe sim -q "error handling"   # Natural language search
```

### Context Generation (orca#1685)

```
snipe context [path]          # Generate Claude-optimized project context
snipe context --format=yaml   # Output as YAML (default: JSON)
snipe context --full          # Include all symbols, not just key ones
```

**Output Structure:**

```json
{
  "project": {
    "name": "snipe",
    "root": "/path/to/repo",
    "lang": ["go"],
    "build": "mage",
    "test": "mage test"
  },
  "architecture": {
    "components": [
      {
        "name": "indexer",
        "purpose": "Static analysis via go/packages",
        "entry": "cmd/index.go",
        "key_files": ["internal/index/*.go"]
      }
    ],
    "data_flows": ["CLI → index → SQLite → query → output"]
  },
  "files": {
    "by_concern": {
      "storage": {
        "internal/store/store.go": "SQLite operations"
      }
    }
  },
  "symbols": {
    "types": [
      {"name": "Response", "file": "internal/output/types.go", "line": 23}
    ],
    "functions": [
      {"name": "LookupByID", "file": "internal/query/lookup.go", "line": 45}
    ]
  },
  "meta": {
    "generated_at": "2026-01-16T...",
    "git_commit": "abc123",
    "index_fingerprint": "..."
  }
}
```

**Key Design Decisions:**
- Leverages existing index data (symbols, files, call graph)
- Adds heuristics for "key files" (high reference count, entry points)
- Components inferred from package structure + call graph clustering
- Freshness tied to index fingerprint

### Utilities

```
snipe doctor                    # Check dependencies, index health
snipe config <key> [value]      # Get/set configuration
```

### Global Flags

```
--json          # JSON output (default)
--human         # Pretty-printed for debugging
--limit N       # Cap results (default: 50)
--context N     # Lines of context around match
--with-body     # Include full enclosing function body
```

---

## Input Addressing

Commands accept multiple input forms, resolved in order:

| Form | Example | Resolution |
|------|---------|------------|
| Position | `--at file.go:42:12` | Exact location (most reliable) |
| Qualified | `pkg/path.Symbol` | Package path + name |
| Method | `(*T).Method` or `T.Method` | Receiver + method |
| Bare name | `ProcessOrder` | Search, return candidates if ambiguous |

When ambiguous, return `candidates[]` not a silent guess:

```json
{
  "error": {
    "code": "AMBIGUOUS_SYMBOL",
    "message": "Multiple definitions found for 'Config'",
    "candidates": [
      { "id": "abc", "name": "Config", "file": "config/config.go", "kind": "type" },
      { "id": "def", "name": "Config", "file": "server/config.go", "kind": "type" }
    ]
  }
}
```

---

## Output Schema

### Determinism Contract

All commands return:

```json
{
  "results": [...],
  "meta": {
    "command": "refs",
    "query": { "symbol": "ProcessOrder" },
    "repo_root": "/path/to/repo",
    "index_state": "fresh|stale|missing",
    "degraded": [],
    "ms": 23,
    "total": 7,
    "truncated": false,
    "token_estimate": 1250
  },
  "error": null
}
```

On error:

```json
{
  "results": null,
  "meta": { ... },
  "error": {
    "code": "MISSING_INDEX",
    "message": "No index found. Run 'snipe index' first to build it (takes ~5 seconds for most Go projects).",
    "next": { "command": "snipe index" }
  }
}
```

### Result Schema

```json
{
  "id": "a3f2c1",
  "file": "order/handler.go",
  "range": {
    "start": { "line": 42, "col": 4 },
    "end": { "line": 42, "col": 31 }
  },
  "kind": "call",
  "name": "ProcessOrder",
  "match": "ProcessOrder(ctx, req.Order)",
  "enclosing": {
    "id": "b4e2d1",
    "kind": "func",
    "name": "HandleCheckout",
    "signature": "func HandleCheckout(ctx context.Context, req *Request) error",
    "range": {
      "start": { "line": 38, "col": 0 },
      "end": { "line": 52, "col": 1 }
    }
  },
  "context": {
    "before": ["if req.Valid() {"],
    "after": ["if err != nil {", "  return fmt.Errorf(...)"]
  },
  "edit_target": "order/handler.go:42:4-42:31"
}
```

### Edit-Ready Output

The `edit_target` field provides handoff format: `file:line:col-line:col`

For batch operations:

```bash
snipe refs ProcessOrder | jq '.results[].edit_target'
```

---

## Graceful Degradation

| Condition | Behavior | meta.degraded |
|-----------|----------|---------------|
| No index | `search` uses rg; LSP commands fail with actionable error | `["rg_only"]` |
| Stale index | Commands work, flag staleness | `["stale_index"]` |
| rg missing | `search` fails with install instructions | - |
| Embeddings unconfigured | `sim` fails with config instructions | - |

---

## Configuration

Global: `~/.config/snipe/config.json`
Project: `.snipe.json` (overrides global)

```json
{
  "index": {
    "exclude": ["vendor", "node_modules", "testdata", ".git"],
    "cache_dir": "~/.cache/snipe"
  },
  "embeddings": {
    "provider": "ollama",
    "model": "nomic-embed-text",
    "api_key": null
  },
  "output": {
    "context_lines": 3,
    "default_limit": 50
  }
}
```

Embedding providers:
- `ollama` (default if available, model: `nomic-embed-text`)
- `openai` (requires api_key)
- `gemini` (requires api_key)
- `claude` (requires api_key)

---

## Dependencies

| Component | Strategy |
|-----------|----------|
| rg | Expect installed, error with install instructions if missing |
| go/packages, go/types | Compiled in (adds ~15MB to binary) |
| SQLite | Compiled in (go-sqlite3 or modernc.org/sqlite) |
| Embeddings | Optional, configured per provider |

---

## Workflow Optimization

Based on observed patterns:

| Pattern | Optimization |
|---------|--------------|
| nav → show → edit | Include `enclosing` + `range` in every result |
| refs → pick → edit | Stable `id` enables `snipe show --id X` |
| refs → batch edit | `edit_target` field directly pipeable |
| repeated queries | Result IDs cached, `snipe show --id` instant |

### Compound Commands

```bash
# Get refs with full function bodies
snipe refs ProcessOrder --with-body

# Prep for batch edit
snipe refs OldName --edit-prep | edit-tool --rename NewName

# Chain: find → show → edit
snipe def Config --at main.go:15:8 | snipe show --stdin | edit-tool
```

---

## Phases

| Phase | Scope | Target |
|-------|-------|--------|
| 1 | index, search, def, refs | MVP: core navigation |
| 2 | callers, callees, impl, type, show | Complete static analysis |
| 3 | sim (semantic search) | Embeddings integration |
| 4 | TS/JS support | tsserver integration |

---

## Success Criteria

- [ ] `snipe refs X` < 100ms for 100k LOC repo (warm)
- [ ] `snipe index` < 30s for 100k LOC
- [ ] Output directly usable by Claude without post-processing
- [ ] Works fully without embeddings configured
- [ ] Zero external runtime dependencies (single binary)

---

## Open Questions

1. **Pure Go SQLite?** `modernc.org/sqlite` (pure Go, slower) vs `go-sqlite3` (CGO, faster, harder cross-compile)
2. **Incremental indexing granularity?** File-level vs package-level
3. **Result pagination?** Cursor-based for large result sets?

---

## Interface Dispatch Limitation

Static analysis cannot resolve dynamic dispatch:

```go
func Process(r io.Reader) {
    r.Read(buf)  // Resolves to io.Reader.Read, not concrete impl
}
```

Mitigation:
- `callers` of interface method shows "calls via interface"
- `impl` command shows all implementing types
- User can narrow down; usually sufficient

---

## Related

- Companion edit tool (future, separate binary)
- Orca integration (MCP wrapper if needed for Claude Desktop)
