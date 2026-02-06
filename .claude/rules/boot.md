sha: 9fc3861
updated: 2026-02-06T18:15:00Z
qa: pass
intent: fix snipe correctness for Claude — wave 2 first, presentation removed

ready: implement wave 2 (correctness fixes) from docs/feedback/BENCHMARK.md
- wave 1 (presentation) eliminated — gummy.go deleted, --human removed, JSON-only now
- wave 2 is now highest priority — these directly affect Claude's answers
- plan first, then implement, then `mage qa`, then re-test with docs/feedback/TESTING_PROMPT.md

north star: optimize snipe for Claude, not humans. JSON-only output.

wave 2 (correctness — do these first, in order):
  1. pkg main resolution: resolve "main" and "." to correct pkg_path
     - avg score 3.3, lowest scoring command
     - internal/query/lookup.go:782
  2. pack on structs: aggregate method call graphs
     - avg score 6.7, returns 0 callers/callees for types
     - cmd/pack.go:278, need FindMethodsOfType query
  3. boot context: type-aware scoring + exclude examples/
     - avg score 6.0, Claude's orientation misses key types
     - internal/context/ranking.go, roles.go:179
  4. importers short name resolution (same pkg_path fix as #1)

wave 3 (quality — after wave 2):
  5. impl method-set matching (internal/query/lookup.go:756) — avg 5.5
  6. pkg grouping by kind (cmd/pkg.go)
  7. --no-body struct fields (kind-aware flag behavior)
  8. explain/types empty fallback

key docs:
- docs/feedback/BENCHMARK.md — full benchmark with scores, dimensions, test proposals
- docs/feedback/TESTING_PROMPT.md — structured testing prompt for next round

done:
- removed entire human output layer: gummy.go (1285 LOC), human.go (100 LOC)
- removed --human flag, TTY auto-detect, fatih/color, golang.org/x/term deps
- simplified Writer (2 params), GetOutputConfig (6 returns), JSON-only WriteResponse
- reprioritized waves: correctness before presentation (Claude is the customer)
- mage qa pass, 75 files changed, -7991 LOC

prior-session:
- 3 rounds LLM testing, benchmark with scores, wave plan
- --help grouping, README rewrite, incremental indexing, hex IDs
