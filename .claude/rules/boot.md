# Boot
updated: 2026-04-28

→ no immediate do-next — binary testing in progress before any release

state: main @ f3cd836, `make audit` green

✓ done
- sandbox ported from trixi (go-sandbox v0.2.0, linux/amd64 prebuilts committed)
- #126 audit sweep: all F/M findings pre-implemented in current codebase; 43/50 beads closed
- behavioral golden tests: 15 commands pinned (def/callers/callees/refs/impl/pack/types/tests/impact/lifecycle/deps/pkg)
- brew release (snipe-b4b) deferred to 2026-06-01 — binary needs more testing first

‡ traps
- audit issues vs current code: always reproduce before filing beads — #126 was v0.1.0-era, codebase had moved past everything
- golden tests require UPDATE_GOLDENS=1 go test -tags blackbox ./test/blackbox/ -run TestGolden to regenerate after intentional output changes
