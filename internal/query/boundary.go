package query

import (
	"database/sql"
	"fmt"
	"strings"
)

// MatchPackagePatterns returns the subset of `universe` matching any pattern.
// A pattern is either an exact suffix (e.g. "internal/store") or a recursive
// suffix ending in "/..." (e.g. "internal/store/..."), which matches the
// package itself and any descendant.
//
// Matching is suffix-based against the full pkg_path so callers can use short
// forms — `internal/store` matches `github.com/x/proj/internal/store`.
func MatchPackagePatterns(universe, patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(universe))
	var out []string
	for _, pkg := range universe {
		for _, pat := range patterns {
			if matches(pkg, pat) {
				if _, dup := seen[pkg]; !dup {
					seen[pkg] = struct{}{}
					out = append(out, pkg)
				}
				break
			}
		}
	}
	return out
}

func matches(pkg, pattern string) bool {
	if strings.HasSuffix(pattern, "/...") {
		base := strings.TrimSuffix(pattern, "/...")
		return strings.HasSuffix(pkg, "/"+base) || pkg == base ||
			strings.Contains(pkg, "/"+base+"/")
	}
	return pkg == pattern || strings.HasSuffix(pkg, "/"+pattern)
}

// BoundaryRef is one symbol whose refs cross between two sets, plus locations.
type BoundaryRef struct {
	Symbol    string        // name (no receiver prefix)
	Kind      string        // func | method | type | ...
	SourcePkg string        // package containing the ref site (enclosing)
	TargetPkg string        // package containing the symbol definition
	RefCount  int           // total cross-boundary refs to this symbol
	Locations []BoundaryLoc // detailed per-ref sites; populated when requested
}

// BoundaryLoc is a single ref site (file:line in the SOURCE package).
type BoundaryLoc struct {
	File string
	Line int
}

// BoundaryReport groups crossings by direction. AToB = refs whose enclosing
// is in set A and target is in set B. BToA is the reverse.
type BoundaryReport struct {
	SetA []string // resolved pkg_paths in set A
	SetB []string // resolved pkg_paths in set B
	AToB []BoundaryRef
	BToA []BoundaryRef
}

// FindBoundaryCrossings runs the 3-way join and returns a report.
// setA and setB are full pkg_paths (already resolved via MatchPackagePatterns).
// Locations are not populated — call PopulateBoundaryLocations for that.
func FindBoundaryCrossings(db *sql.DB, setA, setB []string) (*BoundaryReport, error) {
	if len(setA) == 0 || len(setB) == 0 {
		return &BoundaryReport{SetA: setA, SetB: setB}, nil
	}

	atob, err := queryDirection(db, setA, setB)
	if err != nil {
		return nil, fmt.Errorf("A→B: %w", err)
	}
	btoa, err := queryDirection(db, setB, setA)
	if err != nil {
		return nil, fmt.Errorf("B→A: %w", err)
	}

	return &BoundaryReport{SetA: setA, SetB: setB, AToB: atob, BToA: btoa}, nil
}

// queryDirection finds refs where enclosing.pkg ∈ src AND target.pkg ∈ dst.
// Aggregated by target symbol so repeated call sites become a count.
func queryDirection(db *sql.DB, srcPkgs, dstPkgs []string) ([]BoundaryRef, error) {
	srcPlace := placeholders(len(srcPkgs))
	dstPlace := placeholders(len(dstPkgs))

	q := fmt.Sprintf(`
		SELECT tgt.name, tgt.kind, enc.pkg_path, tgt.pkg_path, COUNT(*) AS n
		FROM refs r
		JOIN symbols tgt ON tgt.id = r.symbol_id
		JOIN symbols enc ON enc.id = r.enclosing_id
		WHERE enc.pkg_path IN (%s)
		  AND tgt.pkg_path IN (%s)
		  AND enc.pkg_path != tgt.pkg_path
		GROUP BY tgt.id
		ORDER BY n DESC, tgt.name ASC
	`, srcPlace, dstPlace)

	args := make([]any, 0, len(srcPkgs)+len(dstPkgs))
	for _, p := range srcPkgs {
		args = append(args, p)
	}
	for _, p := range dstPkgs {
		args = append(args, p)
	}

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BoundaryRef
	for rows.Next() {
		var b BoundaryRef
		if err := rows.Scan(&b.Symbol, &b.Kind, &b.SourcePkg, &b.TargetPkg, &b.RefCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// PopulateBoundaryLocations fills BoundaryRef.Locations for every entry in
// the report — file_path_rel + line for each crossing ref site. Done as a
// separate pass so the summary path stays cheap.
func PopulateBoundaryLocations(db *sql.DB, report *BoundaryReport) error {
	if report == nil {
		return nil
	}
	if err := fillLocations(db, report.AToB, report.SetA, report.SetB); err != nil {
		return fmt.Errorf("A→B: %w", err)
	}
	if err := fillLocations(db, report.BToA, report.SetB, report.SetA); err != nil {
		return fmt.Errorf("B→A: %w", err)
	}
	return nil
}

func fillLocations(db *sql.DB, refs []BoundaryRef, srcPkgs, dstPkgs []string) error {
	if len(refs) == 0 {
		return nil
	}
	srcPlace := placeholders(len(srcPkgs))
	dstPlace := placeholders(len(dstPkgs))

	q := fmt.Sprintf(`
		SELECT tgt.name, r.file_path_rel, r.line
		FROM refs r
		JOIN symbols tgt ON tgt.id = r.symbol_id
		JOIN symbols enc ON enc.id = r.enclosing_id
		WHERE enc.pkg_path IN (%s)
		  AND tgt.pkg_path IN (%s)
		  AND enc.pkg_path != tgt.pkg_path
		ORDER BY r.file_path_rel, r.line
	`, srcPlace, dstPlace)

	args := make([]any, 0, len(srcPkgs)+len(dstPkgs))
	for _, p := range srcPkgs {
		args = append(args, p)
	}
	for _, p := range dstPkgs {
		args = append(args, p)
	}

	rows, err := db.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	bySymbol := make(map[string]*BoundaryRef, len(refs))
	for i := range refs {
		bySymbol[refs[i].Symbol] = &refs[i]
	}

	for rows.Next() {
		var name, file string
		var line int
		if err := rows.Scan(&name, &file, &line); err != nil {
			return err
		}
		if br, ok := bySymbol[name]; ok {
			br.Locations = append(br.Locations, BoundaryLoc{File: file, Line: line})
		}
	}
	return rows.Err()
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
