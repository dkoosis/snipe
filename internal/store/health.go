package store

import (
	"fmt"
)

// IntegrityCheck runs SQLite's PRAGMA integrity_check and returns its first
// result row: "ok" for a healthy database, otherwise the first problem found.
func (s *Store) IntegrityCheck() (string, error) {
	var result string
	if err := s.db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return "", err // unwrapped: snipe doctor prints it in the check details
	}
	return result, nil
}

// OrphanedRefCount returns the number of refs rows whose symbol_id names no
// row in symbols.
func (s *Store) OrphanedRefCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM refs WHERE symbol_id NOT IN (SELECT id FROM symbols)`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count orphaned refs: %w", err)
	}
	return n, nil
}

// FileFuncCounts returns per-file function+method counts for production code
// (_test.go excluded), keyed by file_path_rel.
func (s *Store) FileFuncCounts() (map[string]int, error) {
	rows, err := s.db.Query(`
		SELECT file_path_rel, COUNT(*)
		FROM symbols
		WHERE kind IN ('func', 'method')
		  AND file_path_rel IS NOT NULL
		  AND file_path_rel NOT LIKE '%_test.go'
		GROUP BY file_path_rel`)
	if err != nil {
		return nil, fmt.Errorf("query file func counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]int)
	for rows.Next() {
		var path string
		var n int
		if err := rows.Scan(&path, &n); err != nil {
			return nil, fmt.Errorf("scan func count row: %w", err)
		}
		out[path] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate func count rows: %w", err)
	}
	return out, nil
}
