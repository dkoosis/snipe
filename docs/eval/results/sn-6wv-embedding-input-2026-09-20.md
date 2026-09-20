# sn-6wv: richer embedding input — before/after (2026-09-20)

Change: the text embedded per symbol went from `name + signature + doc` to that
plus `package <short path>: <first sentence of package doc>` plus
`called by: <up to 3 distinct non-test callers>` (`cmd/embedtext.go`). Only the
input to the embedder changed; resolution rungs (exact / case-insensitive /
method-by-name) and the search/sim code are untouched.

## Caveat: proxy embedder, not Voyage

`SNIPE_VOYAGE_API_KEY` in the build environment returned 401 (invalid) and the
keychain holds no key, so Voyage could not be called. Both runs below used a
local stand-in served at `VOYAGE_API_URL`: hashed bag-of-words, 1024 dims,
camelCase-aware tokens, L2-normalised. It scores lexical overlap between the
query and the embedded text, which is what the added package/caller text
changes; it is NOT a semantic model. **Re-run with a valid Voyage key before
treating the delta as a Voyage result** (steps below).

## Corpus

chi, cobra, bbolt, fzf (`--depth 1` clones of default branches, 2026-09-20),
each `snipe index --force --embed-mode=realtime` with embeddings, once with the
pre-change binary (main @ d6e900b) and once with the change. Embedded symbols:
chi 615, cobra 905, bbolt 1466, fzf 1825 (same symbol set both sides; only
the text differs).

## Semantic rung: vague-intent probe

30 natural-language queries (8 chi, 8 cobra, 8 bbolt, 6 fzf), each with the
symbol name(s) that should surface; `snipe sim <q> --threshold 0 --limit 20`.

| | MRR@20 | hit@1 | hit@5 |
|---|---|---|---|
| before | 0.374 | 8/30 | 13/30 |
| after  | 0.395 | 8/30 | 16/30 |

20 queries moved: 13 improved, 7 got worse (e.g. cobra "work out which subcommand
the arguments refer to": 20 -> miss; bbolt "verify the database is not
corrupted": 15 -> 19). Small, mixed, N=30: directional only.

## Deterministic rungs / existing harness

`go test -tags eval ./test/eval/` over the same four repos, before and after:
identical. File 100.0%, symbol 100.0%, efficiency 97.7%, mean MRR 0.75
(floor 0.72). The harness's own caveat line ("Semantic ranking NOT exercised:
zero embeddings") fires in both runs even though the indexes hold embeddings:
`countEmbeddings` reads `embed-status` batch totals, which realtime mode does
not set. It is a harness counting quirk, not an empty index; `sim` returns
results from these indexes.

## Re-run with Voyage

```
# build base (git archive main) and change binaries; two copies of each corpus repo
export SNIPE_VOYAGE_API_KEY=<valid key>
snipe index --force --embed-mode=realtime      # in each corpus repo, each binary
snipe sim "<query>" --threshold 0 --limit 20   # score rank of expected symbol
go test -tags eval ./test/eval/                # per tree
```
