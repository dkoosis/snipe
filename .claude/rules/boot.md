sha: c344076 | qa: pass | updated: 2026-03-19

## Do next

Pick one: #127 un-skip impl test | #117 semsearch unit tests | #128 v0.1.1 tag

## Done

- #123 root detection: FindProjectRoot in internal/util, OpenStore resolves git root, doctor mismatch check
- #121 search index fallback: LookupByName fallback between rg and semantic, renamed no_index → rg_only
- Simplify review: search.go resolves git root for store, cached Exists in OpenStore, renamed flag
- Renamed VOYAGE_API_KEY → SNIPE_VOYAGE_API_KEY throughout
- Full /sweep simplify + craft across all 17 packages

## Backlog

#127 un-skip impl test | #112 vague-intent search | #117 semsearch unit tests | #128 v0.1.1 tag | #129 Homebrew tap | #122 human/table format | #126 trixi audit

## Traps

- Credentials file uses SNIPE_VOYAGE_API_KEY (renamed 2026-03-18) — old key name will silently not work
- Orca fixed — passes `--format json` now. If snipe's JSON envelope changes, re-verify orca.
- `knownSubcommands` map in cmd/root.go must be updated when adding new commands
- No interfaces in snipe's own source — impl can only be tested via blackbox fixture
- Do NOT optimize eval until telemetry provides ground truth
- search.go bypasses OpenStore (by design — works without index). Root resolution added separately in simplify review.
