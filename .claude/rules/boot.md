# Boot
updated: 2026-05-06

→ next: `bd ready` — pick the top P2/P3. snipe-84h is the lone P2 but needs interface-method-call-site analysis (substantial design); P3s are smaller wins.

✓ done
- 8 beads: lifecycle cluster (dhq, 29j, 13u, 0sb, 45t, ct7, 36f) + context arch warnings (8xk)

‡ traps
- New `lifecycle.Role` value → update exhaustive switch in `cmd/diagram.go:edgeFromType`
- New BootContext field that adds a `## section` → regen golden via `UPDATE_GOLDENS=1 go test -tags blackbox ./test/blackbox/ -run TestGolden_Context`
