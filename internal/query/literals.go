package query

import (
	"database/sql"
	"fmt"
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
