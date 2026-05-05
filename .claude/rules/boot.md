# Boot
updated: 2026-05-05

→ next: `bd ready` and pick — snipe-96h arch-metrics suite has children unblocked (snipe-cat abstractness, snipe-gq4 LCOM4, snipe-fac complexity rollup); snipe-g63 distance-from-main-sequence now unblocked by snipe-dn4

✓ done
- snipe-orh: Ca/Ce coupling metrics (`snipe metrics --kind=coupling`); production-only filter excludes _test.go + .test pseudo-pkgs
- snipe-dn4: instability metric (`snipe metrics --kind=instability`); persisted alongside ca/ce in computeImportCoupling

‡ traps
- new metrics: register kind in cmd/metrics.go switch + run `go test ./cmd -run TestHelpGolden -update`
- index metrics only run on `--force` or full reindex; incremental skips them
