# snipe

Go code nav CLI for LLMs. Static indexing, <50ms queries, JSON out.

## verify

`mage` (build+lint+test) or `mage qa` (full: race, blackbox, govulncheck)

## layout

```
cmd/           CLI commands (def, refs, callers, callees, search, index)
internal/
  index/       go/packages indexing
  query/       symbol lookup, position resolution
  store/       SQLite persistence
  context/     boot context, roles, flows, enrichment
  embed/       Voyage AI embeddings (batch + realtime)
test/blackbox/ integration tests
.snipe/        local index (gitignored)
```

## usage

```
snipe index                  # build index (embeddings + enrichment by default)
snipe index --enrich=false   # skip LLM enrichment
snipe index --embed-mode=off # skip embeddings
snipe def Symbol             # definition by name
snipe def --at file:L:C      # definition at position
snipe refs/callers/callees   # graph traversal
snipe show <hex-id>          # expand by 16-char ID
snipe search "pattern"       # ripgrep fallback
snipe context --boot         # LLM boot context with entry points, flows, boundaries
```

## focus

Replace go_symbol + Explore agents with snipe.

streams: A=feature parity, B=boot context #1685, C=reliability, D=kg_hints

deep context: `search_nugs(id: "n:project:snipe-evolution-v2")`

## session

`n:boot:snipe` tracks: state (active|wrapped), working_on, ready (issue#)

## invariants

- `mage qa` passes before merge
- output: `{results, meta, error}` JSON
- hex IDs: 16-char, chain across commands
