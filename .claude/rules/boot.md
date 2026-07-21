# Boot

## lane: DeepCaiman
updated: 2026-07-21

↑ so-that: sharpen `snipe plan`/`verify` into a diff-native edit worklist Claude trusts before touching Go — serves ★ (Claude fluent in a repo without Explore agents).

→ next: no open PRs. sn-8f6q epic open, plan v1 landed. Pick from ready: sn-8f6q.4 (uncovered+ripple warnings) · .6 (golden churn detection) · .8 (test-file refs past FindTests' 2-hop) · snipe-p61 (txtar CLI coverage) · sn-5xen (optional read-tx). Medium → dispatch-bead-agent.

‡ traps:
- git-fixture tests set `core.hooksPath=/dev/null` — keep it on any new throwaway-repo test or the global commit-msg hook rejects fixture commits.
- tests that exec the `snipe` binary must set `KEYRING_DISABLE` — else an unlocked-keychain `/usr/bin/security` exec can prompt/hang the suite.

~ rapport: dk steers fork-by-fork, merges decisively on green.
