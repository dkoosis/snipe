# sn-6wv: richer embedding input — before/after on Voyage (2026-09-20)

**Verdict: modestly better, not conclusively. On real Voyage (voyage-code-3), MRR@20 rose 0.364 -> 0.445 and hit@1 6 -> 9 of 30, but hit@5 did not move (19 -> 19) and 5 of 30 queries got worse (10 better, 15 unchanged). About 1.8 of the 2.8 reciprocal-rank gained comes from 3 queries moving to rank 1; at N=30 a 10-vs-5 split is not distinguishable from noise (two-sided sign test p ~ 0.30).** This replaces the earlier lexical-proxy run (MRR 0.374 -> 0.395), which was not a Voyage result.

Change: the text embedded per symbol went from `name + signature + doc` to that
plus `package <short path>: <first sentence of package doc>` plus
`called by: <up to 3 distinct non-test callers>` (`cmd/embedtext.go`). Only the
input to the embedder changed; resolution rungs (exact / case-insensitive /
method-by-name) and the search/sim code are untouched.

## Setup

- Embedder: real Voyage `voyage-code-3` via `SNIPE_VOYAGE_API_KEY` (status-checked HTTP 200 before the run).
- Before binary: `git archive origin/main` (8342b74; the only commit past the PR's merge base d6e900b is an output/telemetry-only change, #251, which cannot affect embedding text or `sim`). After binary: this PR branch at 89736b4.
- Corpus (`--depth 1` clones, 2026-09-20): chi 3d1777a, cobra adbc881, bbolt 4dc08f7, fzf b1be3a8. One copy of each repo per binary, each indexed with `snipe index --force --embed-mode=realtime`. Embedded symbols: chi 615, cobra 905, bbolt 1466, fzf 1825 (4,811 per binary, identical symbol set both sides; only the text differs). Test-file symbols are in the index and compete for ranks.
- Probe: `docs/eval/sn-6wv-probe.tsv` — 30 vague-intent queries (8 chi, 8 cobra, 8 bbolt, 6 fzf), each with the expected symbol short name(s), authored by reading the corpus source before any Voyage result was seen and not edited afterwards. Same queries both sides. Run and score with `docs/eval/sn-6wv-probe.py`; raw ranks in `sn-6wv-probe-results-2026-09-20.json`.
- Scoring: `snipe sim "<q>" --threshold 0 --limit 20 --format json`; rank = best-ranked expected name among the top 20, miss = beyond 20 (counts 0 in MRR).
- Query-side reproducibility: re-running the before-side probe gave identical ranks for all 30 queries. Index-side run-to-run variation was not measured (each index was built once; spend cap).

## Result

| | MRR@20 | hit@1 | hit@5 |
|---|---|---|---|
| before | 0.364 | 6/30 | 19/30 |
| after  | 0.445 | 9/30 | 19/30 |

Per repo (MRR@20): chi 0.314 -> 0.396, cobra 0.414 -> 0.507, bbolt 0.397 -> 0.468, fzf 0.319 -> 0.398. Every repo moved up, hit@5 is flat in each.

Improved 10, worse 5, unchanged 15.

- Biggest wins: cobra "force the user to supply a certain option..." 6 -> 1, bbolt "run a read-only operation against a consistent snapshot" 2 -> 1, fzf "read candidate lines..." 2 -> 1; bbolt "shrink the file..." miss -> 11 and fzf "remember previously typed queries..." miss -> 18 are real but low-rank recoveries.
- Regressions: fzf "avoid rescanning everything..." 12 -> miss, bbolt "walk the whole file looking for inconsistencies" 11 -> 15, cobra "produce reference documentation pages..." 3 -> 6, cobra "figure out which nested command the user typed" 3 -> 4, chi "make a trailing slash in the URL not matter" 5 -> 6.
- Unmoved misses in both runs: chi compress, cobra tab-completion, bbolt Batch, fzf FuzzyMatchV2 scoring.

### Per query

| repo | query | expected | before | after | |
|---|---|---|---|---|---|
| chi | stop requests that take too long to finish | Timeout | 4 | 2 | better |
| chi | limit how many requests are handled at the same time | Throttle, ThrottleBacklog, ThrottleWithOpts | 2 | 2 | same |
| chi | work out the real address of the caller when behind a proxy | RealIP, ClientIPFromXFF, ClientIPFromHeader, GetClientIP | 9 | 3 | better |
| chi | recover from a crash inside a handler and return an error page | Recoverer | 4 | 3 | better |
| chi | tag every incoming request with a unique identifier | RequestID, NextRequestID | 1 | 1 | same |
| chi | make a trailing slash in the URL not matter | StripSlashes, RedirectSlashes | 5 | 6 | WORSE |
| chi | shrink the response body for clients that support it | Compress, NewCompressor | miss | miss | same |
| chi | attach a separate sub-application under a URL path prefix | Mount | 5 | 3 | better |
| cobra | force the user to supply a certain option before the command runs | MarkFlagRequired, MarkPersistentFlagRequired, ValidateRequiredFlags | 6 | 1 | better |
| cobra | reject the invocation when the wrong number of positional arguments is given | ExactArgs, RangeArgs, MinimumNArgs, MaximumNArgs | 7 | 7 | same |
| cobra | propose similar command names when the user mistypes one | SuggestionsFor, findSuggestions | 1 | 1 | same |
| cobra | let users press tab to fill in the values of an option | RegisterFlagCompletionFunc | miss | miss | same |
| cobra | produce reference documentation pages for every command | GenMarkdownTree, GenManTree, GenYamlTree, GenReSTTree | 3 | 6 | WORSE |
| cobra | prevent two options from being used at the same time | MarkFlagsMutuallyExclusive | 1 | 1 | same |
| cobra | figure out which nested command the user typed | Find, Traverse | 3 | 4 | WORSE |
| cobra | run some setup code before any command executes | OnInitialize | 3 | 2 | better |
| bbolt | walk the whole file looking for inconsistencies | Check, checkFunc | 11 | 15 | WORSE |
| bbolt | shrink the file by rewriting live data into a fresh database | Compact | miss | 11 | better |
| bbolt | run a read-only operation against a consistent snapshot | View | 2 | 1 | better |
| bbolt | combine several small writes from many goroutines into one transaction | Batch | miss | miss | same |
| bbolt | jump to the first entry at or after a given key | Seek | 4 | 4 | same |
| bbolt | hand out an ever-increasing number for numbering records | NextSequence | 1 | 1 | same |
| bbolt | make a backup copy of the data while it is in use | CopyFile, Copy, WriteTo | 3 | 3 | same |
| bbolt | make sure only one process can use the data file at a time | flock | 1 | 1 | same |
| fzf | score how well a candidate matches the typed characters, favouring word boundaries | FuzzyMatchV2, FuzzyMatchV1, bonusFor | miss | miss | same |
| fzf | turn the command line arguments into a configuration | ParseOptions, parseOptions | 1 | 1 | same |
| fzf | read candidate lines from standard input or by walking a directory tree | ReadSource, readFiles | 2 | 1 | better |
| fzf | remember previously typed queries between sessions | NewHistory | miss | 18 | better |
| fzf | avoid rescanning everything when one more character is typed | ChunkCache, NewChunkCache, Lookup | 12 | miss | WORSE |
| fzf | split each line into fields using a separator | Tokenize, awkTokenizer | 3 | 3 | same |

## Deterministic rungs / existing harness

Not re-run here. The builder ran `go test -tags eval ./test/eval/` over the same four repos before and after: identical (file 100.0%, symbol 100.0%, efficiency 97.7%, mean MRR 0.75, floor 0.72). The harness's "Semantic ranking NOT exercised: zero embeddings" caveat fires falsely on realtime-mode indexes because `countEmbeddings` reads `embed-status` batch totals, which realtime mode does not set; the indexes above do hold embeddings and `sim` returns results from them.

## Reproduce

```
# build base (git archive origin/main) and change binaries; one copy of each corpus repo per binary
export KEYRING_DISABLE=1   # SNIPE_VOYAGE_API_KEY from your environment
snipe index --force --embed-mode=realtime      # in each corpus repo, each binary
python3 docs/eval/sn-6wv-probe.py run <snipe-base> <base-corpus-dir> before.json
python3 docs/eval/sn-6wv-probe.py run <snipe-new>  <new-corpus-dir>  after.json
python3 docs/eval/sn-6wv-probe.py score before.json after.json
```
