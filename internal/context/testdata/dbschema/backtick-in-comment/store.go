package store

// Use `go test` to run tests; use `mage qa` for the full quality gate.
// A stray `backtick` in a comment must not swallow the real DDL below.
const schema = `
CREATE TABLE IF NOT EXISTS widgets (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL
);
`

// query uses a double-quoted string with an embedded backtick: "`SELECT 1`".
var query = "`SELECT 1`"
