# Plan: Context Enrichment for snipe

**Goal:** Make `snipe context` output sufficient to replace Explore agents. Claude should be able to orient on a codebase without spawning subagents to read files.

---

## Key Insight

Explore agents exist to answer *questions*, not to enumerate symbols. Per-symbol enrichment alone won't replace them. The output must provide:

1. **Orientation** - Where does this start? What are the main flows?
2. **Ranking** - Which symbols matter? (not just which are referenced most)
3. **Structure** - How do components relate?

---

## Questions Answered First

### Model Selection

```yaml
recommended: claude-3-5-haiku
rationale:
  - task: summarization, not complex reasoning
  - input: function signature + body (~50-200 tokens avg)
  - output: 1-line purpose (~10-20 tokens)
  - cost: ~$0.25/1M input, $1.25/1M output
  - estimate: 1000 symbols × 150 tokens = 150K tokens = ~$0.04/index
alternative: gpt-4o-mini (similar cost, OpenAI batch API is 50% off)
avoid: opus/sonnet (overkill for this task)
```

### Enhancing Inline Source Comments

**No.** Keep purposes in the index, not in source.

```yaml
decision: store in index, don't touch source
rationale:
  - modifies user code (invasive)
  - go doc format is specific (must start with symbol name)
  - users didn't ask for this
future: optional `snipe suggest-docs` command that proposes comments for review
```

---

## Current State

What `snipe context --boot` outputs today:

```yaml
project: snipe
lang: go
build: mage
key_symbols:
  - name: Store
    file: internal/store/store.go
    line: 13
  # no purpose, no role, no relationships, no ranking
```

What's missing:

```yaml
missing:
  - boot views: entry points, primary flows, change boundaries
  - ranking: architectural weight, not just ref_count
  - purpose: "what does this symbol DO"
  - role: architectural classification
  - package summaries: what each package owns
```

---

## Phase 1: Role Inference + Interface Signals

**Human summary:** Classify symbols by architectural role using static analysis. This must come first because role informs symbol selection/ranking in Phase 2.

```yaml
scope:
  - infer role from call patterns, naming, and interface implementations
  - add `role` and `visibility` fields
  - detect standard library interface implementations

roles:
  entry_point: "called by main/init, is main, or is cobra.Command.RunE"
  api_boundary: "exported AND has callers from other packages"
  persistence: "package path contains 'store' OR calls sql.DB methods"
  handler: "implements http.Handler OR signature matches http handler"
  io_primitive: "implements io.Reader, io.Writer, io.Closer"
  factory: "name starts with New, returns pointer"
  internal: "unexported or only called within package"

interface_signals:
  - http.Handler → handler
  - io.Reader/Writer/Closer → io_primitive
  - error → error_type
  - fmt.Stringer → stringer
  - sort.Interface → sortable

visibility:
  - exported: starts with uppercase
  - package_private: starts with lowercase

effort: 2-3 hours
files_touched:
  - internal/context/roles.go (new file)
  - internal/context/interfaces.go (new file)
```

---

## Phase 2: Ranked Output with Boot Views

**Human summary:** Reshape output to answer orientation questions. Use role-weighted ranking instead of raw ref_count. Add boot views for entry points, flows, and change boundaries.

### Symbol Selection Algorithm

```yaml
problem: ref_count favors utility functions over architectural pillars
  - parseString: 100 refs (utility)
  - Server: 5 refs (architectural core)

fix: role-weighted ranking
  formula: priority = (log(ref_count) + 1) * role_weight

role_weights:
  entry_point: 10
  api_boundary: 5
  persistence: 5
  handler: 5
  factory: 3
  internal: 1

output_limits:
  top_symbols_overall: 30
  top_per_package: 5-10
  hide long-tail unless --full requested
```

### Boot Views

Three first-class views that answer orientation questions:

```yaml
1_entry_points:
  what: main packages, cobra commands, handlers, jobs
  for_each: purpose + immediate callees
  answers: "where does this start?"

2_primary_flows:
  what: top 5-10 call paths from entry points
  format: "cmd/index → indexer.Run → store.Write → output.Format"
  answers: "what's the spine of this codebase?"

3_change_boundaries:
  what: where to modify behavior, grouped by concern
  categories:
    - persistence: store package, sql calls
    - cli_handling: cmd package, cobra commands
    - output_formatting: output package
  ranked_by: fan-in and cross-package impact
  answers: "where do I change X behavior?"
```

### Package Summaries

```yaml
scope:
  - 1-line purpose per package (from package doc or inferred)
  - aggregate role counts: "5 structs (persistence), 2 interfaces"
  - show as header before package's symbols

benefit: Claude reasons "this package owns X" without digging
```

### Output Shape

```yaml
output_after:
  boot_views:
    entry_points:
      - name: main
        file: cmd/snipe/main.go:10
        purpose: "CLI entry, initializes cobra"
        callees: [rootCmd.Execute]
      - name: indexCmd.RunE
        file: cmd/index.go:25
        purpose: "builds symbol index from go/packages"
        callees: [index.Build, store.Write]

    primary_flows:
      - "cmd/index → index.Build → store.WriteIndex"
      - "cmd/def → query.DefinitionByName → store.GetSymbol"
      - "cmd/refs → query.References → store.GetRefs"

    change_boundaries:
      persistence:
        package: internal/store
        key_symbols: [Store, WriteIndex, GetSymbol]
      cli:
        package: cmd
        key_symbols: [rootCmd, defCmd, refsCmd]
      output:
        package: internal/output
        key_symbols: [Format, JSON, Human]

  packages:
    - name: internal/store
      purpose: "SQLite persistence for symbol index"
      roles: {persistence: 5, factory: 2}
      top_symbols:
        - name: Store
          kind: struct
          purpose: "manages the SQLite index database"
          sig: "type Store struct"
          refs: 27
          role: persistence

effort: 4-6 hours
files_touched:
  - internal/context/generate.go (reshape output, add boot views)
  - internal/context/types.go (add fields)
  - internal/context/ranking.go (new file, selection algorithm)
  - internal/context/flows.go (new file, call path extraction)
```

---

## Phase 3: LLM-Generated Purposes

**Human summary:** For symbols without doc comments, generate 1-line purposes using an LLM. Use content hashing for incremental enrichment.

```yaml
scope:
  - add `snipe index --enrich` flag
  - batch undocumented symbols to LLM
  - store purposes with content hash for incremental updates
  - include in context output

incremental_enrichment:
  storage:
    table: symbol_purposes
    columns: [symbol_id, purpose, content_hash, model, generated_at]

  content_hash: sha256(signature + body)

  on_index:
    - calculate hash for each undocumented symbol
    - skip LLM call if hash matches stored purpose
    - regenerate if hash changed
    - prune purposes for deleted symbols

  benefit: only re-enrich changed code, ~$0.01 for incremental vs $0.03 for full

prompt: |
  Given this Go symbol, provide a 1-line purpose (max 10 words).
  Do not start with "This function" or similar. Be terse.

  Symbol: {name}
  Kind: {kind}
  Signature: {signature}
  Body:
  ```go
  {body_truncated_to_100_lines}
  ```

  Purpose:

cost_estimate:
  symbols_without_docs: ~450 (46% of 1000)
  tokens_per_symbol: ~200 input, ~15 output
  full_index: ~$0.03
  incremental: ~$0.01 (typical)

effort: 4-6 hours
files_touched:
  - internal/context/enrich.go (new file, LLM calls)
  - internal/store/schema.go (add purposes table)
  - cmd/index.go (add --enrich flag)
```

---

## Phase 4: Architecture Summary

**Human summary:** Generate architecture description grounded in call graph data. LLM describes, but doesn't invent.

```yaml
scope:
  - analyze package structure and cross-package calls
  - generate architecture description anchored to concrete data
  - store as project-level metadata

structure:
  spine:
    what: primary call flows (from Phase 2)
    source: static analysis, not LLM

  components:
    what: package ownership and boundaries
    source: aggregated from role inference

  edges:
    what: top cross-package calls with counts
    source: call_graph table
    example: "store → sql (47 calls), cmd → query (23 calls)"

  description:
    what: LLM-generated prose connecting the above
    grounded_in: spine, components, edges (passed as context)

prompt: |
  Describe this Go project's architecture in 3-5 sentences.
  Focus on how components connect. Be terse.

  Primary flows:
  {spine}

  Package purposes:
  {components}

  Cross-package calls:
  {edges}

  Architecture:

benefit: architecture summary aligns with symbol- and flow-level views
effort: 2-3 hours
cost: ~$0.01 per index (one LLM call)
```

---

## Execution Order

```yaml
order:
  1: phase_1  # role inference - must come first for ranking
  2: phase_2  # ranked output + boot views - biggest impact
  3: phase_3  # LLM enrichment - fills gaps
  4: phase_4  # architecture summary - polish

rationale:
  - phase 1 before 2: roles inform symbol selection algorithm
  - phase 2 is highest impact: boot views replace Explore agents
  - phase 3 after 2: enrichment targets already-selected symbols
  - phase 4 last: cherry on top, depends on all prior phases

total_effort: 12-18 hours
total_cost_per_index: ~$0.05 (phases 3+4)
```

---

## Success Criteria

```yaml
before:
  - claude spawns Explore agent to understand codebase
  - agent makes 10-20 tool calls
  - first question often: "where does this start?"

after:
  - `snipe context --boot` provides orientation in 1 call
  - entry points, flows, change boundaries answered upfront
  - purposes for >95% of ranked symbols

measurable:
  primary:
    - Explore agent spawn rate for orientation: <20% (down from ~80%)
    - median tool calls during first-response orientation: 1-2 (down from 10-20)

  secondary:
    - % of sessions with follow-up "where is X / how does this start": <10%
    - spot-check accuracy of generated purposes: >90%

  tracking:
    - log agent spawns with reason
    - sample orientation sessions monthly
    - review purpose accuracy quarterly
```
