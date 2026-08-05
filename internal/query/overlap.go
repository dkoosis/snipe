package query

import (
	"context"
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

// mergeRanges normalizes a file's line ranges — swapping inverted bounds, then
// sorting and coalescing overlapping or adjacent spans — so the overlap query
// emits one predicate per distinct span instead of one per raw hunk. Fewer
// predicates means fewer bind parameters and a shallower OR tree on large diffs.
func mergeRanges(ranges []LineRange) []LineRange {
	if len(ranges) == 0 {
		return nil
	}
	norm := make([]LineRange, len(ranges))
	for i, r := range ranges {
		if r.End < r.Start {
			r.Start, r.End = r.End, r.Start
		}
		norm[i] = r
	}
	sort.Slice(norm, func(i, j int) bool {
		if norm[i].Start != norm[j].Start {
			return norm[i].Start < norm[j].Start
		}
		return norm[i].End < norm[j].End
	})
	merged := norm[:1]
	for _, r := range norm[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End+1 { // overlapping or directly adjacent
			if r.End > last.End {
				last.End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

// overlapBatchPredicates caps the number of OR-leaf predicates per batched
// query. Each flattened predicate binds at most 3 params (a fresh range
// binds path+end+start; a stale file binds path alone), so a batch binds at
// most 3 * 250 = 750 params — under the vendored SQLite host-parameter limit
// (999) with headroom, far under the modern default (32766). The OR-spine is
// at most 250 leaves deep plus a small constant per leaf, keeping expression
// tree depth (~253) under MAX_EXPR_DEPTH=1000. It's a var, not a const, so
// tests can lower it to force multi-batch behavior on small inputs.
var overlapBatchPredicates = 250

// overlapQueryPrefix and overlapQuerySuffix bracket each batch's WHERE
// clause. Kept as a single, uninterrupted backtick literal each (not built
// via concatenation) so orderby_guard_test.go's raw-source scan sees the
// full "ORDER BY s.file_path, s.line_start, s.id" clause intact.
const overlapQueryPrefix = `
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE `

const overlapQuerySuffix = `
		ORDER BY s.file_path, s.line_start, s.id`

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

	// Flatten every predicate — one per stale file, one per merged fresh
	// range — into parallel slices instead of building a single combined
	// WHERE. IN(a,b) is semantically identical to path=a OR path=b, so
	// dropping the combined stale IN(...) clause changes nothing but shape.
	// Flattening lets stale and fresh predicates share one uniform batching
	// loop below, and puts a hard, countable cap on params/depth per batch.
	var predSQL []string
	var predArgs [][]any

	for _, p := range paths {
		if _, ok := staleSet[toRelPath(p, repoRoot)]; ok {
			predSQL = append(predSQL, "s.file_path = ?")
			predArgs = append(predArgs, []any{p})
			continue
		}
		for _, r := range mergeRanges(changes[p]) {
			predSQL = append(predSQL, "(s.file_path = ? AND s.line_start <= ? AND s.line_end >= ?)")
			predArgs = append(predArgs, []any{p, r.End, r.Start})
		}
	}

	if len(predSQL) == 0 {
		return nil, nil
	}

	// Wrap the whole batch loop in one read transaction so every batch reads
	// from a single, stable snapshot. Without it each batch runs as its own
	// implicit transaction; a concurrent writer (e.g. a reindex in WAL mode)
	// committing between two batches would straddle the write boundary and
	// could double-count or drop a symbol. modernc.org/sqlite honors
	// ReadOnly:true as a plain deferred BEGIN, which takes the read snapshot on
	// the first query and holds it until Rollback. The loop only reads, so
	// Rollback (never Commit) is correct and cheaper — a read-only tx has
	// nothing to commit; the deferred Rollback also runs on any early error
	// return below. This consistency guarantee is structural — verified by
	// construction, not by a (necessarily flaky) mid-loop-writer timing test.
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true}) //nolint:forbidigo // FindOverlappingSymbols has no ctx param; threading one is the pre-existing noctx debt tracked separately, out of this pack-adoption's scope
	if err != nil {
		return nil, fmt.Errorf("query overlapping symbols: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only tx: nothing to persist, rollback is cleanup-only

	var allRows []SymbolRow
	for start := 0; start < len(predSQL); start += overlapBatchPredicates {
		end := start + overlapBatchPredicates
		if end > len(predSQL) {
			end = len(predSQL)
		}

		query := overlapQueryPrefix + strings.Join(predSQL[start:end], " OR ") + overlapQuerySuffix

		var args []any
		for _, a := range predArgs[start:end] {
			args = append(args, a...)
		}

		rows, err := tx.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("query overlapping symbols: %w", err)
		}
		batch, err := scanSymbolRows(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		allRows = append(allRows, batch...)
	}

	// Dedupe by ID: symbols.id is a PRIMARY KEY and files.path is a PRIMARY
	// KEY, so the LEFT JOIN yields exactly one row per matching symbol.
	// Batches can only re-surface the same row (e.g. a symbol spanning
	// hunks split across two batches) — never multiply it — so a seen-set
	// on ID is a safe, sufficient dedupe.
	seen := make(map[string]struct{}, len(allRows))
	result := make([]SymbolRow, 0, len(allRows))
	for i := range allRows {
		if _, ok := seen[allRows[i].ID]; ok {
			continue
		}
		seen[allRows[i].ID] = struct{}{}
		result = append(result, allRows[i])
	}

	// Each batch is locally ordered by the per-query ORDER BY, but the
	// merged stream across batches is not globally ordered. Re-sort in Go
	// by (FilePath, LineStart, ID) to reproduce the single-query
	// ORDER BY s.file_path, s.line_start, s.id exactly. ID must stay in the
	// comparator: two symbols can share (FilePath, LineStart) (e.g.
	// `var a, b int`), and only ID makes this a total order matching SQL.
	sort.Slice(result, func(i, j int) bool {
		if result[i].FilePath != result[j].FilePath {
			return result[i].FilePath < result[j].FilePath
		}
		if result[i].LineStart != result[j].LineStart {
			return result[i].LineStart < result[j].LineStart
		}
		return result[i].ID < result[j].ID
	})

	return result, nil
}
