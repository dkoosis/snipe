# Boot
updated: 2026-04-28

→ Review test/blackbox/testdata/golden/ — confirm output looks correct before treating it as ground truth

state: main @ f4a7998, `make audit` green

✓ done
- 20 behavioral golden tests pinned (all commands including context + diagram)
- diagram flow walk() sorted deterministically (bug fix)

‡ traps
- diagram goldens strip D2 source — hex node IDs non-deterministic; only tree summary is pinned
- golden regen: UPDATE_GOLDENS=1 go test -tags blackbox ./test/blackbox/ -run TestGolden
