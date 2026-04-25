# Boot
updated: 2026-04-24

→ pick next from `bd ready`. zhs.4 shipped.

state: main @ 4a95c9f, `make audit` green

✓ done
- feat(cmd): `snipe boundary <a> <b>` — module-split planning query
  - 3-way SQL join (refs ⨝ symbols target ⨝ symbols enclosing)
  - exact + `pkg/...` recursive package patterns
  - `--detailed` adds per-ref file:line; `--direction=both|a-to-b|b-to-a`
  - perf: 44ms / 31ms on snipe (target was 500ms)
  - blackbox: cmd→query crossings + `--detailed` locations
- fix(test): regenerated 2 stale goldens (post-`file_path_rel` fix)
- fix(cmd): registered `boundary` in `knownSubcommands`
