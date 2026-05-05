# Boot
updated: 2026-05-05

→ next: `bd ready` and pick — snipe-g63 distance-from-main-sequence now fully unblocked (needs both A + I); also snipe-gq4 LCOM4, snipe-fac complexity rollup under snipe-96h

✓ done
- snipe-orh: Ca/Ce coupling metrics (`snipe metrics --kind=coupling`); production-only filter excludes _test.go + .test pseudo-pkgs
- snipe-dn4: instability metric (`snipe metrics --kind=instability`); persisted alongside ca/ce in computeImportCoupling
- snipe-cat: abstractness metric (`snipe metrics --kind=abstractness`); A = interface decls / (interface+struct+type), exported only, production files only

‡ traps
- new metrics: register kind in cmd/metrics.go switch + run `go test ./cmd -run TestHelpGolden -update`
- index metrics only run on `--force` or full reindex; incremental skips them
