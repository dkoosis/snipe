sha: 3bdd0fa
updated: 2026-02-07T00:35:00Z
qa: pass (@ 3bdd0fa + local fixes)
intent: use trustworthy benchmark to improve snipe's localization accuracy

ready: analyze benchmark failures and improve snipe output quality
  - harness is now trustworthy: every FAIL = real snipe limitation
  - baseline: 55.9% symbol accuracy on 34 scored tasks, 3 known gaps excluded
  - targets: file >90%, symbol >75%, efficiency >80% (currently 85/56/97)
  - run: `mage Eval` to benchmark, results in docs/eval/EVAL_RESULTS.json
  - diff: docs/eval/task_status.txt tracks PASS/FAIL changes between runs
  - failure categories to investigate:
    - callers: file-miss on orca callers (3/6 fail) — check symbol resolution
    - search: qualified symbol matching misses (Daemon.Run, Scanner.ScanWithMeta)
    - pkg: orca pkg tasks missing symbols (Router, registerTool, Classifier)
    - cross-cutting: multi-step trace gaps (VoyageEmbedder, maxEmbeddingTextLen)
  - approach: pick highest-impact category, fix snipe, re-benchmark, iterate

key context:
  - benchmark YAML: docs/eval/benchmark.yaml (37 tasks, 5 repos)
  - scoring logic: test/eval/score.go (known_gap, candidate promotion, two-pronged matching)
  - task status: docs/eval/task_status.txt (per-task PASS/FAIL/SKIP)
  - eval results: docs/eval/EVAL_RESULTS.json (full JSON report)
  - Agentless paper: https://arxiv.org/abs/2407.01489 (localization metrics)

done (this session):
- merged PRs #89 (concurrency review), #90 (KG hint tests), #91 (SQLite hardening)
- fixed race condition: RLock -> Lock for accessTime write in internal/util/file.go
- fixed lint: import grouping and stale nolint directives in internal/store/write.go
- added eval harness targets (mage EvalSetup, mage Eval)
- cleanup: stale docs, .DS_Store in gitignore, remote branch prune
- mage qa: all green (370 tests, 0 races, 0 vulns, 0 lint issues)

prior-session:
- eval harness: receiver on callers/callees, scoring fixes, YAML corrections
- established quality direction, researched Agentless/SWE-bench benchmarks
- wave 2 correctness fixes, removed human output layer (-7991 LOC)
