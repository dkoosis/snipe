# Boot
updated: 2026-05-09

→ next: `bd ready` and pick. Lint sweeps shipped — config now valid v2, gocritic perf tag enabled.

✓ done
- 391388c: rangeValCopy ×95 + small gocritic (preferStringWriter, appendCombine, equalFold)
- e298823: goconst ×308 + .golangci.yml v1→v2 migration

‡ traps
- gocritic hugeParam disabled (27 findings need signature changes — snipe-5hu)
