# snipe

Make it easier for Claude to work with Go repos. Static indexing, <50ms queries, output optimized for Claude.

## verify

`make` (vet+lint+test) or `make audit` (full: race, blackbox, govulncheck)

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
snipe context                # Claude-optimized orientation (entry points, flows, boundaries)
snipe context --full         # Full architecture dump
snipe lifecycle <Type>       # CRUD trace: Create/Mutate/Read/Delete funcs + caller chains
snipe lifecycle T --include-tests  # fold _test.go refs into role buckets (default: separate)
```

## focus

Replace go_symbol + Explore agents with snipe.

streams: A=feature parity, B=boot context #1685, C=reliability, D=kg_hints

deep context: `search_nugs(id: "n:project:snipe-evolution-v2")`

## session

`n:boot:snipe` tracks: state (active|wrapped), working_on, ready (issue#)

## decisions

◯ Do not revisit.

| ID | Principle | Constraint |
|----|-----------|------------|
| D1 | Claude is the primary consumer | Default output optimized for Claude, not humans or generic JSON parsers |
| D2 | One command should "just work" | Bare name lookup → fuzzy match → method fallback → semantic. No syntax guessing |
| D3 | Index root = git root | Always resolve to repo root, not CWD. Eliminates MISSING_INDEX confusion |
| D4 | Token budget is a first-class concern | Every byte of output must earn its place. No envelope noise in default output |
| D5 | Hex IDs chain across commands | 16-char IDs enable follow-up queries without re-lookup |
| D6 | `make audit` is the merge gate | No exceptions |

## invariants

- `make audit` passes before merge
- hex IDs: 16-char, chain across commands

## lifecycle traps

- Indexer ∅ `enclosing_id` on signature-line refs (`func F() *T`). Recovered via `reattachSignatureRefs` in `cmd/lifecycle.go`
- R2 snippet regex: `func F() *T {` matches `T{` — guard with `isFuncDeclLine` before firing create rules
- `BASELINE_ORCA.json` timestamp drifts on every run; ✗ stage it
- New cobra subcommand → register in `knownSubcommands` map (`cmd/root.go`) or `snipe X` routes to `sym` (bare-symbol fallback)
- httptest blocking handlers: observe a stop chan, not just `r.Context().Done()` — `server.Close()` WaitGroups on the handler
- Ranking SQL/sort with non-unique keys (e.g. same name across pkgs): always include `file_path` (or equivalent) as tiebreaker — golden tests will flake otherwise
