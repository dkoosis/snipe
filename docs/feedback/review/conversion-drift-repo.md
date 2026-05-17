# conversion-drift — snipe (repo scope)

run-id: ecebe5258308
date: 2026-05-17
linter: conversion-drift (diff-scoped by design)
scope requested: project
findings: 0

## Scope note

The `conversion-drift` linter is explicitly **diff-scoped** (see linter prelude: "Don't run this against full repo — it's diff-scoped by design"). The bug class it detects — a conversion helper whose zero/empty/nil semantics quietly changed — only exists across a diff. There is no project-scope signal: a stable helper that has always mapped `zero → NULL` is not drift, it is the contract.

Running it project-wide is therefore a category error. This report documents the substitute pass performed and why no findings were emitted.

## Substitute pass

To honor the project-scope request without fabricating findings, I scanned for the **inputs** the linter would normally consume:

| Probe | Result |
|---|---|
| Custom `Scan` / `Value` / `MarshalJSON` / `UnmarshalJSON` in non-vendor code | none — snipe owns no custom marshaler/scanner methods |
| `sql.Null*` usage sites | 9 files (`cmd/deadcode.go`, `cmd/lifecycle.go`, `internal/query/{lookup,tests,types,imports}.go`, `internal/context/{generate,roles}.go`, `cmd/lits_test.go`) — all read-side: `var x sql.NullString` followed by `if x.Valid`. No helper indirection that could drift. |
| `omitempty` additions in last 10 commits | only `GitCommit` and `IndexFingerprint` on `cmd/orient.go` (new struct, not a tag change on an existing field — no compat surface) |
| `go.mod` serialization-touching dep bumps (pgx, sqlx, gorm, protobuf, JSON forks) | none in last 10 commits |
| Last 10 commits touching conversion helpers | none |

## Why zero findings is the correct output

- snipe has no DB driver indirection layer. SQL access is direct `database/sql` with `sql.Null*` scanned inline at the call site — there is no `*ToSQL` / `*FromSQL` helper that could change semantics.
- JSON encoding uses stdlib `encoding/json` with struct tags; no custom `MarshalJSON`/`UnmarshalJSON` pair exists to go asymmetric.
- The recent diff window (HEAD~10..HEAD) is dominated by CLI plumbing, help text, and new commands (`orient`, `boundary`, `sim --pairs`). No serialization boundary was touched.

## Recommendation

Re-run as diff-scoped on a future PR that touches `internal/store/`, `internal/query/lookup.go` scan helpers, or bumps `modernc.org/sqlite`. Those are the boundaries where this linter pays off for snipe.

Until then, the relevant whole-repo linter for the adjacent class is `zero-sentinel` (Go type level), not `conversion-drift` (diff-on-helper level) — see the linter's own "Don't" section.
