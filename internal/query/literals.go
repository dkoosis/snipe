package query

import (
	"database/sql"
	"fmt"
	"strings"
)

// LiteralRef is a resolved string_ref row returned by queries.
type LiteralRef struct {
	ID          string
	Value       string
	Name        string
	Kind        string
	FilePath    string
	FilePathRel string
	Line        int
	Col         int
	EnclosingID string
	Snippet     string
}

// FindLiteralRefs returns all locations where the given string value appears
// as a string literal (env var call or named const).
func FindLiteralRefs(db *sql.DB, value string) ([]LiteralRef, error) {
	rows, err := db.Query(`
		SELECT id, value, COALESCE(name,''), kind, file_path, COALESCE(file_path_rel,''),
		       line, col, COALESCE(enclosing_id,''), COALESCE(snippet,'')
		FROM string_refs
		WHERE value = ?
		ORDER BY file_path, line, col
	`, value)
	if err != nil {
		return nil, fmt.Errorf("query string_refs: %w", err)
	}
	defer rows.Close()

	var out []LiteralRef
	for rows.Next() {
		var r LiteralRef
		if err := rows.Scan(&r.ID, &r.Value, &r.Name, &r.Kind, &r.FilePath, &r.FilePathRel,
			&r.Line, &r.Col, &r.EnclosingID, &r.Snippet); err != nil {
			return nil, fmt.Errorf("scan string_ref: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FindChurnLiterals returns the distinct golden/testdata fixture paths named as
// string literals in the given test files — the fixtures a symbol change is
// likely to force Claude to regenerate. Results are deduped by value, sorted by
// value, and capped at limit; the returned count is how many distinct paths were
// dropped past the cap (0 if none). An empty testFiles returns (nil, 0, nil)
// without querying.
//
// A literal churns if its value contains "testdata/" or ends in ".golden". This
// reuses the string_refs index, which captures only two literal shapes:
// os.Getenv-family args and named string const declarations. Inline fixture-path
// arguments (e.g. golden.Assert(t, got, "testdata/x.golden")) are NOT indexed,
// so churn detection fires only when the path is declared as a const — the tidy,
// common pattern. Broadening the extractor to inline args is out of scope.
func FindChurnLiterals(db *sql.DB, testFiles []string, limit int) (paths []string, truncated int, err error) {
	if len(testFiles) == 0 {
		return nil, 0, nil
	}

	placeholders := strings.Repeat("?,", len(testFiles))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, 0, len(testFiles)+2)
	for _, f := range testFiles {
		args = append(args, f)
	}
	// Neither pattern contains a LIKE metacharacter, so no ESCAPE is needed.
	args = append(args, "%testdata/%", "%.golden")

	// Count the full distinct match set first so "+N more" stays exact while the
	// SELECT below caps what it materializes. Without this, a pathological test
	// file (thousands of testdata/ consts) would stream every row into Go before
	// the cap applied — the cap would bound the output but not the work, defeating
	// the point of limit for exactly the const registries this feature targets.
	countQ := fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT DISTINCT value
			FROM string_refs
			WHERE file_path IN (%s)
			  AND (value LIKE ? OR value LIKE ?)
		)
	`, placeholders)
	var total int
	if err := db.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count churn literals: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	// DISTINCT collapses the same path referenced by two covering tests; ORDER BY
	// value (a plain column) both sorts the output and satisfies the ORDER BY
	// guard test. LIMIT bounds the rows SQLite streams back, not just the slice.
	q := fmt.Sprintf(`
		SELECT DISTINCT value
		FROM string_refs
		WHERE file_path IN (%s)
		  AND (value LIKE ? OR value LIKE ?)
		ORDER BY value
	`, placeholders)
	if limit > 0 {
		q += "\t\tLIMIT ?\n"
		args = append(args, limit)
	}

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query churn literals: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, 0, fmt.Errorf("scan churn literal: %w", err)
		}
		paths = append(paths, value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return paths, total - len(paths), nil
}
