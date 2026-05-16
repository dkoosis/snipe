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
const SchemaVersion = "1.0"
