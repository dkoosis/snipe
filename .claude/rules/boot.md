# Boot
updated: 2026-05-09 (late)

→ next: `bd ready` — 5xw + 7rv + 7xp + abc + m4i remain (P2 bugs); 277 + s5p + f9l (P3 features). Tree green — `make audit` clean.

✓ done (this session)
- triage: closed 8 won't-fix beads (jsp, 1s2, biu, dxc, my2, 84h, 9bi, bvf-superseded)
- atomic-write helper: `util.WriteFileAtomic` — used by edit.ApplyAndWrite + embed.SaveState
- 5 P1 bugs: 52w (Voyage breadcrumb), c7u (poll ctx), 15y (kg subprocess ctx), 0y7 (atomic source), 87f (lock TOCTOU contents-match)
- 3 P2 bugs: idl (atomic state + corruption-recovery), c7p (schema_version in tx), v6c (incremental meta in tx)
- dependency wiring: idl→0y7, v6c→c7p, 2lr→5xw, b1b→5xw

‡ traps
- gocritic hugeParam intentionally disabled — not worth the API churn (snipe-my2 closed won't-fix)
- watch cluster: 5xw must land before 2lr/b1b (deps wired)
