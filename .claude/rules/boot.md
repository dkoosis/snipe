# Boot

## lane: DeepCaiman
updated: 2026-07-20

→ next: no open PRs — #196–#200 all on main. 5 ready: sn-nduv (drop plaintext creds file — follow-up to keyring #200) · sn-8f6q epic (verify+plan: .4 uncovered/ripple, .5 edit worklist) · snipe-p61 (txtar CLI coverage) · sn-5xen (optional read-tx). Medium → dispatch-bead-agent.

✓ done
- v0.2.0 released (ships `snipe risk`). cc-plugins contract closed: judge branches on `.results[0].degraded`, never `len(results)`.
- #199 merged: risk-JSON golden test + doc (sn-n8re, sn-33gg) — cc-plugins now enforces the contract against sn-n8re's guard.
- #200 merged: voyage key keychain-first via `dkoosis/keyring` (sn-8qgp). keyring made **public** (v0.1.0) so `go install …@latest` resolves it.

‡ traps:
- git-fixture tests set `core.hooksPath=/dev/null` — keep it on any new throwaway-repo test or the global commit-msg hook rejects fixture commits.
- tests that exec the `snipe` binary must set `KEYRING_DISABLE` — else an unlocked-keychain `/usr/bin/security` exec can prompt/hang the suite.

~ rapport: dk steers fork-by-fork, merges decisively on green.
