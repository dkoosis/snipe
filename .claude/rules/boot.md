# Boot

## lane: DeepCaiman
updated: 2026-07-19

→ next: merge draft PRs #196 (sn-za8p, @gemini pending) · #197 (sn-v84m) · #198 (sn-j2oy); `bd close` each on merge. Then 5 ready — sn-n8re/sn-33gg (risk-JSON contract test+doc, promised to cc-plugins), sn-8f6q.4 + snipe-p61 (medium → dispatch-bead-agent), sn-5xen (optional).

✓ done
- v0.2.0 released (first tag since v0.1.2; ships `snipe risk`). cc-plugins contract closed: judge branches on `.results[0].degraded`, never `len(results)`.

‡ traps: git-fixture tests now set `core.hooksPath=/dev/null` — keep it on any new throwaway-repo test or the global commit-msg hook rejects fixture commits.

~ rapport: dk steers fork-by-fork, merges decisively on green.
