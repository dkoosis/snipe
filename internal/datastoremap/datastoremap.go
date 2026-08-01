// Package datastoremap joins three signals snipe already collects into one
// answer: for each detected datastore, which packages/functions read it and
// which write it.
//
//  1. internal/c4 detects datastores by import (a package importing a driver
//     from the fixed allowlist) — but that raw signal includes Go external
//     test packages (declared `package foo_test`) as if they were real
//     touchpoints, and can list the same package twice under a generic
//     "SQL" name and a specific driver name.
//  2. internal/context's DetectDBSchemas finds the DDL for a store — a
//     separate detector (grep-shaped, not package-aware) that this package
//     correlates back to a c4 Datastore by directory.
//  3. internal/lifecycle classifies functions that reference a Go type as
//     Create/Mutate/Read/Delete — the read/write direction c4 alone lacks.
//
// This package holds only the pure join/bucketing logic (filter, group,
// match, bucket) so it's unit-testable without a database. DB access —
// finding the candidate types in a package, fetching their refs — stays in
// cmd/datastoremap.go, mirroring how internal/lifecycle stays pure while
// cmd/diagram.go does the querying.
package datastoremap

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/dkoosis/snipe/internal/c4"
	"github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/lifecycle"
)

// Entry is one function's access to a store, projected from a lifecycle
// Classification.
type Entry struct {
	Func string
	File string
	Line int
	Role lifecycle.Role
}

// Store is the access-map render unit: one detected datastore, its schema
// (when a DBSchema correlates to it), and its read/write function lists.
type Store struct {
	Name         string // display name, e.g. "SQLite"
	Package      string // owning package import path, "" if no c4 match
	SchemaSource string // DBSchema.Source, "" if no schema matched
	DDL          string
	Reads        []Entry
	Writes       []Entry
	Unclear      int // non-test classifications that were neither Read nor a write role
}

// Row is one (datastore, importing-package) pair — the per-package view the
// access map is built around. c4.Datastore groups all importers of a driver
// technology under Evidence (one row per tech, since sn-igsn); this package
// flattens that back to one Row per importing package, which is the grain the
// read/write access map needs.
type Row struct {
	Name    string // datastore display name, e.g. "SQLite"
	Package string // one importing package import path
	File    string // datastore detection site (representative, per driver)
	Line    int
}

// FilterTestPackages flattens c4 Datastore rows (grouped by driver, importers
// in Evidence) into one Row per importing package, dropping Go external test
// packages (declared `package foo_test`, compiled only for `go test`). The
// imports table records those importer packages exactly like any real one,
// but they exist only to exercise the store — not a real touchpoint of the
// running program.
func FilterTestPackages(datastores []c4.Datastore) []Row {
	var out []Row
	for _, d := range datastores {
		for _, pkg := range d.Evidence {
			if strings.HasSuffix(pkg, "_test") {
				continue
			}
			out = append(out, Row{Name: d.Name, Package: pkg, File: d.File, Line: d.Line})
		}
	}
	return out
}

// GroupByPackage collapses multiple Rows for the same importing package into
// one Row per package. A package that imports both a generic driver
// ("database/sql") and a specific one (e.g. "modernc.org/sqlite") produces two
// rows for the same package; the specific name is always the more useful
// label, so it wins over the generic "SQL" fallback.
func GroupByPackage(rows []Row) []Row {
	best := make(map[string]Row, len(rows))
	order := make([]string, 0, len(rows))
	for _, r := range rows {
		cur, ok := best[r.Package]
		if !ok {
			best[r.Package] = r
			order = append(order, r.Package)
			continue
		}
		if cur.Name == "SQL" && r.Name != "SQL" {
			best[r.Package] = r
		}
	}
	sort.Strings(order)
	out := make([]Row, 0, len(order))
	for _, pkg := range order {
		out = append(out, best[pkg])
	}
	return out
}

// MatchSchema finds the row whose package directory (relative to modulePath)
// equals the DBSchema's source directory — the heuristic that correlates a
// schema detected by DetectDBSchemas (embedded Go DDL, or a migrations/
// directory) with the c4-detected package that owns it. Returns ("", false)
// when no package resolves to the schema's directory — e.g. a repo-root
// schema.sql isn't owned by any one package.
func MatchSchema(schema context.DBSchema, rows []Row, modulePath string) (string, bool) {
	schemaDir := filepath.ToSlash(filepath.Dir(schema.Source))
	for _, r := range rows {
		pkgDir := strings.TrimPrefix(r.Package, modulePath)
		pkgDir = strings.TrimPrefix(pkgDir, "/")
		if pkgDir != "" && pkgDir == schemaDir {
			return r.Package, true
		}
	}
	return "", false
}

// BucketRoles groups lifecycle classifications into read/write entries.
// Test-file classifications are skipped entirely (they exercise the store,
// they aren't its real read/write surface); Unknown/Type-use classifications
// are counted in unclear rather than silently dropped, so a caller can see
// the join left something unresolved instead of assuming full coverage.
func BucketRoles(classifications []lifecycle.Classification) (reads, writes []Entry, unclear int) {
	for _, c := range classifications {
		if c.IsTestFile {
			continue
		}
		e := Entry{Func: c.EnclosingName, File: c.FileRel, Line: c.Line, Role: c.Role}
		switch c.Role {
		case lifecycle.RoleRead:
			reads = append(reads, e)
		case lifecycle.RoleCreate, lifecycle.RoleMutate, lifecycle.RoleDelete:
			writes = append(writes, e)
		default:
			unclear++
		}
	}
	sortEntries(reads)
	sortEntries(writes)
	return reads, writes, unclear
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].File != es[j].File {
			return es[i].File < es[j].File
		}
		if es[i].Line != es[j].Line {
			return es[i].Line < es[j].Line
		}
		return es[i].Func < es[j].Func
	})
}
