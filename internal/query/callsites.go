package query

import (
	"database/sql"
	"fmt"
)

// FindCallSites returns every non-def, non-concurrency reference to symbolID
// (call sites and func-value uses), each with its enclosing symbol, source
// snippet, and refs.ast_ctx. ASTCtx is "" when the column is NULL (pre-v18
// index) — callers degrade to plain sites. NO SQL LIMIT: returns the full set
// so the caller can group-then-truncate deterministically in Go and report an
// accurate "+N more" footer. Grouping key = the ref file's package (from
// file_path_rel dir), stable for package-level-init refs where enclosing is NULL.
// Ordered by (ref-file relative path, line, col) — plain columns, ORDER-BY-guard
// safe; sorting by file_path_rel keeps every package's refs contiguous.
//
// The `ast_ctx NOT IN ('go','chan')` filter mirrors FindRefs/GetRefCount so
// goroutine/channel self-attributed rows don't masquerade as call sites.
func FindCallSites(db *sql.DB, symbolID string) ([]RefRow, error) {
	rows, err := db.Query(`
		SELECT r.id, r.symbol_id, r.file_path, r.file_path_rel, r.line, r.col, r.enclosing_id, r.snippet, r.ast_ctx,
		       s.name, s.kind, s.signature,
		       (r.file_path GLOB '*_test.go') AS is_test
		FROM refs r
		LEFT JOIN symbols s ON r.enclosing_id = s.id
		WHERE r.symbol_id = ?
		  AND (r.ast_ctx IS NULL OR r.ast_ctx NOT IN ('go','chan'))
		ORDER BY r.file_path_rel, r.line, r.col
	`, symbolID)
	if err != nil {
		return nil, fmt.Errorf("query call sites: %w", err)
	}
	defer rows.Close()

	var refs []RefRow
	for rows.Next() {
		var r RefRow
		var encName, encKind, encSig, filePathRel, astCtx sql.NullString
		var isTest int
		err := rows.Scan(&r.ID, &r.SymbolID, &r.FilePath, &filePathRel, &r.Line, &r.Col, &r.EnclosingID, &r.Snippet, &astCtx,
			&encName, &encKind, &encSig, &isTest)
		if err != nil {
			return nil, fmt.Errorf("scan call site row: %w", err)
		}
		r.FilePathRel = filePathRel.String
		r.ASTCtx = astCtx.String
		r.EnclosingName = encName.String
		r.EnclosingKind = encKind.String
		r.EnclosingSignature = encSig.String
		r.IsTest = isTest != 0
		refs = append(refs, r)
	}

	return refs, rows.Err()
}

// CountCallSites returns (nonTest, testOnly) reference counts, splitting on
// whether the ref's file is *_test.go. Drives the delete zero-caller fast path.
// Applies the same go/chan concurrency filter as FindCallSites so the counts
// match what FindCallSites would render.
func CountCallSites(db *sql.DB, symbolID string) (nonTest, testOnly int, err error) {
	err = db.QueryRow(`
		SELECT
		  COALESCE(SUM(CASE WHEN r.file_path GLOB '*_test.go' THEN 0 ELSE 1 END), 0) AS non_test,
		  COALESCE(SUM(CASE WHEN r.file_path GLOB '*_test.go' THEN 1 ELSE 0 END), 0) AS test_only
		FROM refs r
		WHERE r.symbol_id = ?
		  AND (r.ast_ctx IS NULL OR r.ast_ctx NOT IN ('go','chan'))
	`, symbolID).Scan(&nonTest, &testOnly)
	if err != nil {
		return 0, 0, fmt.Errorf("count call sites: %w", err)
	}
	return nonTest, testOnly, nil
}
