sha: 3ac0dd0
updated: 2026-02-06T21:00:00Z
qa: skipped
intent: fix snipe's presentation and synthesis layers — wave 1 first

ready: implement wave 1 (presentation fixes) from docs/feedback/BENCHMARK.md
- 3 rounds of LLM testing (fzf, vhs, chi) produced quality benchmark with scores
- wave 1 is highest ROI — mechanical fixes that lift 3 commands from "broken" to 8-9/10
- plan first, then implement, then `mage qa`, then re-test with docs/feedback/TESTING_PROMPT.md

wave 1 (presentation — do these first, in order):
  1. human formatters for ExplainResult, TypesResult, EditResponse
     - gummy.go has ZERO handling for these types — all show "1 items"
     - ExplainResult: render purpose, mechanism steps, warnings, caller_context
     - TypesResult: render method table with signatures
     - EditResponse: render colored unified diff
     - key file: internal/output/gummy.go (search for gummyBodyPreview to find existing renderers)
     - types defined at: internal/output/types.go:468 (ExplainResult), types.go (TypesResult, EditResponse in cmd/edit.go)
  2. body truncation threshold
     - gummyBodyPreview = 15 at internal/output/gummy.go:26 — raise to 80
     - no truncation when total results == 1
     - ~20 LOC change
  3. --human as TTY default
     - isatty check on stdout in cmd/root.go GetOutputConfig
     - JSON when piped, human when interactive
     - ~15 LOC change

wave 2 (correctness — after wave 1):
  4. pack on structs: aggregate method call graphs (cmd/pack.go:278, need FindMethodsOfType query)
  5. pkg main resolution: resolve "main" and "." to correct pkg_path (internal/query/lookup.go:782)
  6. boot context: type-aware scoring + exclude examples/ (internal/context/ranking.go, roles.go:179)
  7. importers short name resolution (same pkg_path fix as #5)

wave 3 (quality — after wave 2):
  8. impl method-set matching (internal/query/lookup.go:756)
  9. pkg grouping by kind (cmd/pkg.go)
  10. --no-body struct fields (kind-aware flag behavior)
  11. explain/types empty fallback

key docs:
- docs/feedback/BENCHMARK.md — full benchmark with scores, dimensions, test proposals, telemetry design
- docs/feedback/TESTING_PROMPT.md — structured testing prompt for next round

done:
- 3 rounds of LLM testing (fzf, vhs, chi) with structured testing prompt
- analyzed all feedback, identified 12 issues, organized into 4 waves
- created benchmark with command scores, quality dimensions, regression guards
- designed passive telemetry (session.json extensions) and active feedback options

prior-session:
- --help grouping, README rewrite, first-run UX
- incremental indexing, version contract, suggestions, token budget, hex IDs
