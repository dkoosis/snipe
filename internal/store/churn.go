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

	// Per-type commit counts classified from each commit's Bead-Type
	// trailer (sdlc ADR 0002). Every touching commit lands in exactly one
	// bucket, so these five sum to Commits. BugCommits is the per-file
	// defect count Tornhill approximates by grepping messages for "fix".
	BugCommits     int `json:"bug_commits"`
	FeatureCommits int `json:"feature_commits"`
	ChoreCommits   int `json:"chore_commits"`
	OtherCommits   int `json:"other_commits"`
	UntypedCommits int `json:"untyped_commits"`
}

// churnColumns is the shared SELECT list, in FileChurn field order.
const churnColumns = `path, commits, authors, first_seen, last_changed, score,
	bug_commits, feature_commits, chore_commits, other_commits, untyped_commits`

// ChurnRankBy names the column `ReadFileChurnRanked` sorts on.
type ChurnRankBy string

const (
	ChurnRankCommits ChurnRankBy = "commits"
	ChurnRankBug     ChurnRankBy = "bug"
	ChurnRankFeature ChurnRankBy = "feature"
	ChurnRankChore   ChurnRankBy = "chore"
	ChurnRankScore   ChurnRankBy = "score"
)

// churnOrderBy maps a rank key to a complete ORDER BY clause. Each clause is
// a whole literal (never assembled from a column name) so the repo's ORDER BY
// guard can see the trailing `path ASC` tiebreaker, and so no caller text can
// reach the SQL. An unknown key ranks by commits, the historical default.
func churnOrderBy(by ChurnRankBy) string {
	switch by {
	case ChurnRankBug:
		return `ORDER BY bug_commits DESC, path ASC`
	case ChurnRankFeature:
		return `ORDER BY feature_commits DESC, path ASC`
	case ChurnRankChore:
		return `ORDER BY chore_commits DESC, path ASC`
	case ChurnRankScore:
		return `ORDER BY score DESC, path ASC`
	case ChurnRankCommits:
		return `ORDER BY commits DESC, path ASC`
	default:
		return `ORDER BY commits DESC, path ASC`
	}
}

// ValidChurnRankBy reports whether by names a supported ranking column.
func ValidChurnRankBy(by ChurnRankBy) bool {
	switch by {
	case ChurnRankCommits, ChurnRankBug, ChurnRankFeature, ChurnRankChore, ChurnRankScore:
		return true
	default:
		return false
	}
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
		`INSERT INTO file_churn (path, commits, authors, first_seen, last_changed, score,
			bug_commits, feature_commits, chore_commits, other_commits, untyped_commits)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		if _, err := stmt.Exec(r.Path, r.Commits, r.Authors, r.FirstSeen, r.LastChanged, r.Score,
			r.BugCommits, r.FeatureCommits, r.ChoreCommits, r.OtherCommits, r.UntypedCommits); err != nil {
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
	return s.ReadFileChurnRanked(ChurnRankCommits, n)
}

// ReadFileChurnRanked returns files ranked by the named column descending,
// ties broken by path ascending so the order is deterministic. An unknown
// key ranks by commits. n <= 0 returns all rows.
func (s *Store) ReadFileChurnRanked(by ChurnRankBy, n int) ([]FileChurn, error) {
	base := `SELECT ` + churnColumns + ` FROM file_churn ` + churnOrderBy(by)
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
		if err := rs.Scan(&r.Path, &r.Commits, &r.Authors, &r.FirstSeen, &r.LastChanged, &r.Score,
			&r.BugCommits, &r.FeatureCommits, &r.ChoreCommits, &r.OtherCommits, &r.UntypedCommits); err != nil {
			return nil, fmt.Errorf("scan churn row: %w", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("iterate churn rows: %w", err)
	}
	return out, nil
}
