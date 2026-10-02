package risk

import (
	"database/sql"
	"path/filepath"
	"strings"

	snipecontext "github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

// callerCap bounds the caller walk so a change to a hub symbol stays fast.
const callerCap = 500

// changedSym is a symbol a diff touched, with the package it belongs to.
type changedSym struct {
	id      string
	pkgPath string
}

// Assess measures the diff between base and head: diff → changed symbols →
// the measures snipe's index holds for them. It never errors — when git or the
// index can't answer, it returns a degraded result.
func Assess(s *store.Store, repoRoot, base, head string) Measures {
	if s == nil {
		return Degrade(ChangeStats{}, "index unavailable")
	}
	stream, ok := gitDiff(repoRoot, base, head)
	if !ok {
		return Degrade(ChangeStats{}, "git diff unavailable (not a work tree, unresolved ref, or git absent)")
	}
	changes := parseDiff(stream)
	goFiles := changedGoFiles(changes)
	stats := ChangeStats{Files: len(changes), GoFiles: len(goFiles)}
	if len(goFiles) == 0 {
		return Degrade(stats, "no changed Go files")
	}

	changed := changedSymbols(s.DB(), repoRoot, changes)
	stats.Symbols = len(changed)

	m := Measures{Changed: stats}
	m.Callers, m.CallerFiles = callers(s.DB(), changed)
	m.Importers = importers(s.DB(), changed)
	m.Commits = commits(s, goFiles)
	m.Roles, m.RiskFlags = roles(s.DB(), repoRoot, changed)
	return m
}

// changedSymbols maps changed files to the func/method/type symbols whose body
// overlaps a changed head-side line range. Symbols the index doesn't know (e.g. a
// brand-new file not yet reindexed) simply don't appear.
func changedSymbols(db *sql.DB, repoRoot string, changes []FileChange) []changedSym {
	var out []changedSym
	seen := make(map[string]bool)
	for _, fc := range changes {
		// A _test.go edit is not a change to production code: it contributes
		// no symbols (its file still counts in Changed and Commits).
		if !strings.HasSuffix(fc.Path, ".go") || strings.HasSuffix(fc.Path, "_test.go") || len(fc.LineRanges) == 0 {
			continue
		}
		abs := filepath.Join(repoRoot, fc.Path)
		rows, err := db.Query(`
			SELECT id, pkg_path, line_start, line_end
			FROM symbols
			WHERE file_path = ? AND kind IN ('func', 'method', 'struct', 'interface', 'type')
		`, abs)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id string
			var pkg sql.NullString
			var start, end int
			if err := rows.Scan(&id, &pkg, &start, &end); err != nil {
				continue
			}
			if seen[id] || !overlaps(start, end, fc.LineRanges) {
				continue
			}
			seen[id] = true
			out = append(out, changedSym{id: id, pkgPath: pkg.String})
		}
		_ = rows.Close()
	}
	return out
}

// overlaps reports whether [start,end] intersects any of the ranges.
func overlaps(start, end int, ranges [][2]int) bool {
	for _, r := range ranges {
		if start <= r[1] && r[0] <= end {
			return true
		}
	}
	return false
}

// callers counts the call sites of the changed symbols and the files they are in.
func callers(db *sql.DB, changed []changedSym) (sites, files int) {
	ids := symbolIDs(changed)
	if len(ids) == 0 {
		return 0, 0
	}
	rows, err := query.FindImpactCallersMulti(db, ids, true, callerCap, 0)
	if err != nil {
		return 0, 0
	}
	seen := make(map[string]bool)
	for i := range rows {
		seen[rows[i].FilePathRel] = true
	}
	return len(rows), len(seen)
}

// importers is the most packages importing any one changed package.
func importers(db *sql.DB, changed []changedSym) int {
	most := 0
	for _, p := range distinctPkgs(changed) {
		var n int
		if err := db.QueryRow(`
			SELECT COUNT(DISTINCT importer_pkg) FROM imports
			WHERE pkg_path = ? AND importer_pkg IS NOT NULL AND importer_pkg != ?
		`, p, p).Scan(&n); err == nil && n > most {
			most = n
		}
	}
	return most
}

// commits is the most non-merge commits to any one changed Go file.
func commits(s *store.Store, goFiles []string) int {
	rows, err := s.ReadFileChurnRanked(store.ChurnRankCommits, 0)
	if err != nil {
		return 0
	}
	byPath := make(map[string]int, len(rows))
	for _, r := range rows {
		byPath[r.Path] = r.Commits
	}
	most := 0
	for _, f := range goFiles {
		most = max(most, byPath[f])
	}
	return most
}

// roles counts the changed symbols per architectural role and per risk flag.
func roles(db *sql.DB, repoRoot string, changed []changedSym) (byRole, byFlag map[string]int) {
	byRole, byFlag = map[string]int{}, map[string]int{}
	ids := symbolIDs(changed)
	if len(ids) == 0 {
		return byRole, byFlag
	}
	got, err := snipecontext.RolesForSymbols(db, repoRoot, ids)
	if err != nil {
		return byRole, byFlag
	}
	for _, sr := range got {
		if sr.Role != "" {
			byRole[string(sr.Role)]++
		}
		for _, rf := range sr.RiskFlags {
			byFlag[rf]++
		}
	}
	return byRole, byFlag
}

// distinctPkgs returns the unique, non-empty package paths among changed symbols.
func distinctPkgs(changed []changedSym) []string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range changed {
		if c.pkgPath != "" && !seen[c.pkgPath] {
			seen[c.pkgPath] = true
			out = append(out, c.pkgPath)
		}
	}
	return out
}

// symbolIDs returns the changed symbols' IDs.
func symbolIDs(changed []changedSym) []string {
	out := make([]string, len(changed))
	for i, c := range changed {
		out[i] = c.id
	}
	return out
}
