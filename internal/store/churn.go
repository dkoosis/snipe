package store

import (
	"database/sql"
	"fmt"
)

// FileChurn is the persisted git change-frequency record for one file,
// keyed by a repo-relative path so it joins against symbols.file_path.
// Populated by `snipe index` from git history; see internal/gitchurn.
type FileChurn struct {
	Path        string  `json:"path"`
	Commits     int     `json:"commits"`
	Authors     int     `json:"authors"`
	FirstSeen   string  `json:"first_seen"`
	LastChanged string  `json:"last_changed"`
	Score       float64 `json:"score"`
}

// WriteFileChurn replaces the whole file_churn table with rows in a single
// transaction. Churn is recomputed wholesale from git history each reindex,
// so a full replace keeps the table consistent with HEAD.
func (s *Store) WriteFileChurn(rows []FileChurn) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { rollbackOnError(tx, &err) }()

	if _, err := tx.Exec(`DELETE FROM file_churn`); err != nil {
		return fmt.Errorf("clear file_churn: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO file_churn (path, commits, authors, first_seen, last_changed, score)
		 VALUES (?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		if _, err := stmt.Exec(r.Path, r.Commits, r.Authors, r.FirstSeen, r.LastChanged, r.Score); err != nil {
			return fmt.Errorf("insert churn row %q: %w", r.Path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ReadFileChurnTopN returns files ranked by commit count descending (ties
// broken by path ascending). n <= 0 returns all rows.
func (s *Store) ReadFileChurnTopN(n int) ([]FileChurn, error) {
	const base = `SELECT path, commits, authors, first_seen, last_changed, score
		FROM file_churn ORDER BY commits DESC, path ASC`
	var (
		rs  *sql.Rows
		err error
	)
	if n <= 0 {
		rs, err = s.db.Query(base)
	} else {
		rs, err = s.db.Query(base+` LIMIT ?`, n)
	}
	if err != nil {
		return nil, fmt.Errorf("query file_churn: %w", err)
	}
	defer func() { _ = rs.Close() }()

	var out []FileChurn
	for rs.Next() {
		var r FileChurn
		if err := rs.Scan(&r.Path, &r.Commits, &r.Authors, &r.FirstSeen, &r.LastChanged, &r.Score); err != nil {
			return nil, fmt.Errorf("scan churn row: %w", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("iterate churn rows: %w", err)
	}
	return out, nil
}
