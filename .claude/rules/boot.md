# Boot

## lane: DeepCaiman
updated: 2026-10-17 · main @ 2ac4543

↑ so-that: snipe is the scorer a PR review-routing function reads (sn-qtjl), and every verb is one an agent picks right first time (sn-lvub) — serves ★ (Claude fluent in a repo without Explore agents).

→ next: no open PRs. sn-qtjl: all children merged — have someone who didn't build it check the epic against its goal, then close. Then sn-zvoz (verb-set decision nug, needs dk's ratification) unblocks sn-lvub.5–.8. Other ready: sn-hdpo (risk tells invariant-critical changes apart) · sn-r1do (telemetry → resolver friction; .6 landed in #283).

‡ traps:
- git-fixture tests set `core.hooksPath=/dev/null` — keep it on any new throwaway-repo test or the global commit-msg hook rejects fixture commits.
- tests that exec the `snipe` binary must set `KEYRING_DISABLE` — else an unlocked-keychain `/usr/bin/security` exec can prompt/hang the suite.
- blackbox `run()` must not pass VOYAGE_API_KEY through to `snipe index` (#286) — else the suite submits real Voyage batches.

~ rapport: dk steers fork-by-fork, merges decisively on green.
