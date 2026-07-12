package query

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// LineRange is an inclusive [Start, End] span of changed lines within one
// file — typically one diff hunk. Start and End are 1-based, matching the
// symbols table's line_start/line_end.
type LineRange struct {
	Start int
	End   int
}

// FindOverlappingSymbols returns the symbols whose declared range
// [line_start, line_end] overlaps at least one changed line range, batched
// across every file in changes with a single query. changes maps each
// file's absolute path to the line ranges that changed in it.
//
// Overlap is inclusive on both ends: a hunk that only touches a symbol's
// first or last line still counts, as does a hunk entirely inside a
// symbol's body or a symbol entirely inside a hunk.
//
// Staleness fallback: for each file, CheckPathStaleness compares on-disk
// mtime/hash against what was stored at index time. When a file comes back
// stale, the index's line numbers may no longer line up with the working
// tree, so hunk-precise matching isn't trustworthy — every symbol in that
// file is returned instead. Fresh files get hunk-precise matching.
func FindOverlappingSymbols(db *sql.DB, repoRoot string, changes map[string][]LineRange) ([]SymbolRow, error) {
	if len(changes) == 0 {
		return nil, nil
	}

	paths := make([]string, 0, len(changes))
	for p := range changes {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	sort.Strings(paths)

	staleRel := CheckPathStaleness(db, repoRoot, paths)
	staleSet := make(map[string]struct{}, len(staleRel))
	for _, rel := range staleRel {
		staleSet[rel] = struct{}{}
	}

	var clauses []string
	var args []any

	var staleAbs []string
	for _, p := range paths {
		if _, ok := staleSet[toRelPath(p, repoRoot)]; ok {
			staleAbs = append(staleAbs, p)
		}
	}
	if len(staleAbs) > 0 {
		placeholders := make([]string, len(staleAbs))
		for i, p := range staleAbs {
			placeholders[i] = "?"
			args = append(args, p)
		}
		clauses = append(clauses, "s.file_path IN ("+strings.Join(placeholders, ",")+")")
	}

	for _, p := range paths {
		if _, ok := staleSet[toRelPath(p, repoRoot)]; ok {
			continue // already covered by the stale IN(...) clause above
		}
		ranges := changes[p]
		if len(ranges) == 0 {
			continue
		}
		var rangeClauses []string
		var rangeArgs []any
		for _, r := range ranges {
			start, end := r.Start, r.End
			if end < start {
				start, end = end, start
			}
			rangeClauses = append(rangeClauses, "(s.line_start <= ? AND s.line_end >= ?)")
			rangeArgs = append(rangeArgs, end, start)
		}
		clauses = append(clauses, "(s.file_path = ? AND ("+strings.Join(rangeClauses, " OR ")+"))")
		args = append(args, p)
		args = append(args, rangeArgs...)
	}

	if len(clauses) == 0 {
		return nil, nil
	}

	query := `
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE ` + strings.Join(clauses, " OR ") + `
		ORDER BY s.file_path, s.line_start, s.id`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query overlapping symbols: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}
