package context

// SchemaVersion is the semver-like major.minor version of the snipe context
// output schema (BootContext and ProjectContext). Downstream tools — notably
// lintbrush reviewers parsing 'snipe context --full' — should pin to a major
// and tolerate minor bumps. Breaking changes bump major; additive changes
// (new optional fields) bump minor.
//
// History:
//
//	1.0 — initial declared schema (snipe-yqo). Covers all fields present at
//	      introduction; future additive fields keep 1.x, removals/renames go
//	      to 2.0 with a CHANGELOG entry.
//	1.1 — additive (sn-zd2): SymbolRef.role now populated on `context --full`
//	      symbols (was boot-only), and new optional SymbolRef.risk_flags carries
//	      orthogonal concurrency/security_boundary classes.
//	2.0 — removal (sn-zcn5.3): BootContext.active_work is gone with the
//	      per-query session tracker that fed it.
const SchemaVersion = "2.0"
