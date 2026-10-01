package query

import (
	"database/sql"
)

// DeadExport is an exported symbol with zero references.
type DeadExport struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Pkg     string `json:"pkg"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	RefsAll int    `json:"refs_all"` // count including tests
}

// FindDeadExports returns exported non-test symbols with no references,
// ordered by package then name. With includeTests false, refs from _test.go
// files do not count, and RefsAll reports the all-files total so callers can
// see the gap. pkg, when set, filters by exact, suffix or substring package
// path match. func main is never reported.
func FindDeadExports(db *sql.DB, pkg string, includeTests bool) ([]DeadExport, error) {
	// Exclude go/chan synthetic self-refs (sn-hmz) — symbol_id == enclosing_id
	// for those rows, which would make a goroutine-spawning exported func look
	// referenced and never get flagged dead.
	refsCountExpr := `(SELECT COUNT(*) FROM refs r WHERE r.symbol_id = s.id AND (r.ast_ctx IS NULL OR r.ast_ctx NOT IN ('go','chan'))`
	if !includeTests {
		refsCountExpr += ` AND r.file_path NOT LIKE '%_test.go'`
	}
	refsCountExpr += `)`

	totalCountExpr := `(SELECT COUNT(*) FROM refs r WHERE r.symbol_id = s.id AND (r.ast_ctx IS NULL OR r.ast_ctx NOT IN ('go','chan')))`

	q := `
		SELECT s.id, s.name, s.kind, s.pkg_path, s.file_path_rel, s.line_start, ` + totalCountExpr + ` AS refs_all
		FROM symbols s
		WHERE s.name GLOB '[A-Z]*'
		  AND s.kind IN ('func','method','type','const','var')
		  AND s.file_path NOT LIKE '%_test.go'
		  AND ` + refsCountExpr + ` = 0
		  AND NOT (s.name = 'main' AND s.kind = 'func')`
	args := []any{}
	if pkg != "" {
		q += ` AND (s.pkg_path = ? OR s.pkg_path LIKE ? OR s.pkg_path LIKE ?)`
		args = append(args, pkg, "%/"+pkg, "%"+pkg+"%")
	}
	q += ` ORDER BY s.pkg_path, s.name`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err // unwrapped: snipe deadcode prints it as the error message
	}
	defer func() { _ = rows.Close() }()

	out := []DeadExport{}
	for rows.Next() {
		var r DeadExport
		var fileRel sql.NullString
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Pkg, &fileRel, &r.Line, &r.RefsAll); err != nil {
			continue
		}
		r.File = fileRel.String
		out = append(out, r)
	}
	return out, nil
}
