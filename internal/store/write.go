package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dkoosis/snipe/internal/index"

	"modernc.org/sqlite"
)

// toRelPath converts an absolute file path to a repo-relative path.
// Returns the original path if it can't be made relative.
func toRelPath(absPath, repoRoot string) string {
	if repoRoot == "" {
		return absPath
	}
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return absPath
	}
	// Normalize to forward slashes for consistency
	return strings.ReplaceAll(relPath, "\\", "/")
}

// WriteIndex writes symbols, refs, and call edges to the database.
// This performs a full reindex (truncate + insert).
// The repo_root meta value (if set) is used to compute relative file paths.
func (s *Store) WriteIndex(symbols []index.Symbol, refs []index.Ref, edges []index.CallEdge) error {
	// Get repo root from meta if available (for computing relative paths)
	repoRoot, _ := s.GetMeta("repo_root")

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Rollback is a no-op if already committed; only log unexpected errors
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			// Log unexpected rollback failure (commit succeeded or genuine error)
			_ = rbErr // In production, consider logging this
		}
	}()

	// Build set of valid symbol IDs for filtering refs and edges
	symbolIDs := make(map[string]struct{}, len(symbols))
	for _, sym := range symbols {
		symbolIDs[sym.ID] = struct{}{}
	}

	// Preserve existing embeddings and symbol purposes before clearing data
	if _, err := preserveEmbeddings(tx); err != nil {
		return err
	}
	if _, err := preserveSymbolPurposes(tx); err != nil {
		return err
	}

	// Clear existing data (embeddings/purposes go to temp table, then get restored)
	if err := truncateTables(tx); err != nil {
		return err
	}

	// Write symbols
	if err := writeSymbols(tx, symbols, repoRoot); err != nil {
		return err
	}

	// Restore embeddings and purposes for symbols that still exist
	if _, err := restoreEmbeddings(tx); err != nil {
		return err
	}
	if _, err := restoreSymbolPurposes(tx); err != nil {
		return err
	}

	// Filter and write refs (only those referencing known symbols)
	validRefs := filterRefs(refs, symbolIDs)
	if err := writeRefs(tx, validRefs, repoRoot); err != nil {
		return err
	}

	// Filter and write call edges (only those referencing known symbols)
	validEdges := filterCallEdges(edges, symbolIDs)
	if err := writeCallEdges(tx, validEdges); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// filterRefs returns only refs that reference existing symbols.
func filterRefs(refs []index.Ref, symbolIDs map[string]struct{}) []index.Ref {
	result := make([]index.Ref, 0, len(refs))
	for _, ref := range refs {
		// symbol_id must exist
		if _, ok := symbolIDs[ref.SymbolID]; !ok {
			continue
		}
		// enclosing_id must exist (if set)
		if ref.EnclosingID != "" {
			if _, ok := symbolIDs[ref.EnclosingID]; !ok {
				continue
			}
		}
		result = append(result, ref)
	}
	return result
}

// filterCallEdges returns only edges that reference existing symbols.
func filterCallEdges(edges []index.CallEdge, symbolIDs map[string]struct{}) []index.CallEdge {
	result := make([]index.CallEdge, 0, len(edges))
	for _, edge := range edges {
		if _, ok := symbolIDs[edge.CallerID]; !ok {
			continue
		}
		if _, ok := symbolIDs[edge.CalleeID]; !ok {
			continue
		}
		result = append(result, edge)
	}
	return result
}

func truncateTables(tx *sql.Tx) error {
	// Delete in order: child tables first (refs, call_graph, imports, embeddings, symbol_purposes),
	// then parent (symbols). This respects foreign key constraints.
	// Note: embeddings are preserved via temp table and restored by restoreEmbeddings()
	// Using explicit statements avoids string concatenation patterns that could be unsafe.
	if _, err := tx.Exec("DELETE FROM refs"); err != nil {
		return fmt.Errorf("truncate refs: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM call_graph"); err != nil {
		return fmt.Errorf("truncate call_graph: %w", err)
	}
	if err := deleteOptionalTable(tx, "imports"); err != nil {
		return err
	}
	// Delete embeddings (they'll be restored from temp table after symbols are inserted)
	if err := deleteOptionalTable(tx, "embeddings"); err != nil {
		return err
	}
	// Delete symbol_purposes (enrichment data) - will be regenerated
	if err := deleteOptionalTable(tx, "symbol_purposes"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM symbols"); err != nil {
		return fmt.Errorf("truncate symbols: %w", err)
	}
	return nil
}

// preserveEmbeddings copies existing embeddings to a temp table for restoration after reindex.
// Returns the number of embeddings preserved.
func preserveEmbeddings(tx *sql.Tx) (int64, error) {
	// Create temp table (if embeddings table exists)
	_, err := tx.Exec(`
		CREATE TEMP TABLE IF NOT EXISTS preserved_embeddings AS
		SELECT symbol_id, embedding, model, created_at FROM embeddings WHERE 1=0
	`)
	if err != nil {
		if isNoSuchTableErr(err, "embeddings") {
			return 0, nil
		}
		return 0, fmt.Errorf("create preserved_embeddings temp table: %w", err)
	}

	// Copy current embeddings to temp table
	result, err := tx.Exec(`
		INSERT INTO preserved_embeddings (symbol_id, embedding, model, created_at)
		SELECT symbol_id, embedding, model, created_at FROM embeddings
	`)
	if err != nil {
		return 0, fmt.Errorf("copy embeddings to temp: %w", err)
	}

	count, _ := result.RowsAffected()
	return count, nil
}

// restoreEmbeddings restores embeddings from temp table for symbols that still exist.
// Returns the number of embeddings restored.
func restoreEmbeddings(tx *sql.Tx) (int64, error) {
	// Restore embeddings for symbols that exist in the new index
	result, err := tx.Exec(`
		INSERT OR IGNORE INTO embeddings (symbol_id, embedding, model, created_at)
		SELECT p.symbol_id, p.embedding, p.model, p.created_at
		FROM preserved_embeddings p
		WHERE p.symbol_id IN (SELECT id FROM symbols)
	`)
	if err != nil {
		if isNoSuchTableErr(err, "preserved_embeddings") {
			return 0, nil
		}
		return 0, fmt.Errorf("restore embeddings: %w", err)
	}

	count, _ := result.RowsAffected()

	// Clean up temp table
	if _, err := tx.Exec("DROP TABLE IF EXISTS preserved_embeddings"); err != nil {
		return 0, fmt.Errorf("drop preserved_embeddings temp table: %w", err)
	}

	return count, nil
}

// preserveSymbolPurposes copies existing symbol purposes to a temp table for restoration after reindex.
// Returns the number of purposes preserved.
func preserveSymbolPurposes(tx *sql.Tx) (int64, error) {
	// Create temp table (if symbol_purposes table exists)
	_, err := tx.Exec(`
		CREATE TEMP TABLE IF NOT EXISTS preserved_purposes AS
		SELECT symbol_id, purpose, content_hash, model, generated_at FROM symbol_purposes WHERE 1=0
	`)
	if err != nil {
		if isNoSuchTableErr(err, "symbol_purposes") {
			return 0, nil
		}
		return 0, fmt.Errorf("create preserved_purposes temp table: %w", err)
	}

	// Copy current purposes to temp table
	result, err := tx.Exec(`
		INSERT INTO preserved_purposes (symbol_id, purpose, content_hash, model, generated_at)
		SELECT symbol_id, purpose, content_hash, model, generated_at FROM symbol_purposes
	`)
	if err != nil {
		return 0, fmt.Errorf("copy purposes to temp: %w", err)
	}

	count, _ := result.RowsAffected()
	return count, nil
}

// restoreSymbolPurposes restores symbol purposes from temp table for symbols that still exist.
// Returns the number of purposes restored.
func restoreSymbolPurposes(tx *sql.Tx) (int64, error) {
	// Restore purposes for symbols that exist in the new index
	result, err := tx.Exec(`
		INSERT OR IGNORE INTO symbol_purposes (symbol_id, purpose, content_hash, model, generated_at)
		SELECT p.symbol_id, p.purpose, p.content_hash, p.model, p.generated_at
		FROM preserved_purposes p
		WHERE p.symbol_id IN (SELECT id FROM symbols)
	`)
	if err != nil {
		if isNoSuchTableErr(err, "preserved_purposes") {
			return 0, nil
		}
		return 0, fmt.Errorf("restore symbol purposes: %w", err)
	}

	count, _ := result.RowsAffected()

	// Clean up temp table
	if _, err := tx.Exec("DROP TABLE IF EXISTS preserved_purposes"); err != nil {
		return 0, fmt.Errorf("drop preserved_purposes temp table: %w", err)
	}

	return count, nil
}

func writeSymbols(tx *sql.Tx, symbols []index.Symbol, repoRoot string) error {
	stmt, err := tx.Prepare(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end, name_line, name_col, signature, doc, receiver)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare symbols insert: %w", err)
	}
	defer stmt.Close()

	for _, sym := range symbols {
		relPath := toRelPath(sym.FilePath, repoRoot)
		_, err := stmt.Exec(
			sym.ID,
			sym.Name,
			string(sym.Kind),
			sym.FilePath,
			relPath,
			sym.PkgPath,
			sym.LineStart,
			sym.ColStart,
			sym.LineEnd,
			sym.ColEnd,
			sym.NameLine,
			sym.NameCol,
			sym.Signature,
			sym.Doc,
			sym.Receiver,
		)
		if err != nil {
			return fmt.Errorf("insert symbol %s: %w", sym.Name, err)
		}
	}

	return nil
}

func writeRefs(tx *sql.Tx, refs []index.Ref, repoRoot string) error {
	stmt, err := tx.Prepare(`
		INSERT INTO refs (id, symbol_id, file_path, file_path_rel, line, col, enclosing_id, snippet)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare refs insert: %w", err)
	}
	defer stmt.Close()

	for _, ref := range refs {
		relPath := toRelPath(ref.FilePath, repoRoot)
		_, err := stmt.Exec(
			ref.ID,
			ref.SymbolID,
			ref.FilePath,
			relPath,
			ref.Line,
			ref.Col,
			nullString(ref.EnclosingID),
			ref.Snippet,
		)
		if err != nil {
			return fmt.Errorf("insert ref: %w", err)
		}
	}

	return nil
}

func writeCallEdges(tx *sql.Tx, edges []index.CallEdge) error {
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO call_graph (caller_id, callee_id, file_path, line, col)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare call_graph insert: %w", err)
	}
	defer stmt.Close()

	for _, edge := range edges {
		_, err := stmt.Exec(
			edge.CallerID,
			edge.CalleeID,
			edge.FilePath,
			edge.Line,
			edge.Col,
		)
		if err != nil {
			return fmt.Errorf("insert call edge: %w", err)
		}
	}

	return nil
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// WriteImports writes import records to the database
func (s *Store) WriteImports(imports []index.Import) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			_ = rbErr
		}
	}()

	if err := writeImports(tx, imports); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func writeImports(tx *sql.Tx, imports []index.Import) error {
	stmt, err := tx.Prepare(`
		INSERT INTO imports (file_path, pkg_path, name, line, col, importer_pkg)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare imports insert: %w", err)
	}
	defer stmt.Close()

	for _, imp := range imports {
		_, err := stmt.Exec(
			imp.FilePath,
			imp.PkgPath,
			nullString(imp.Name),
			imp.Line,
			imp.Col,
			nullString(imp.ImporterPkg),
		)
		if err != nil {
			return fmt.Errorf("insert import %s: %w", imp.PkgPath, err)
		}
	}

	return nil
}

// WriteFiles writes file metadata to the database
func (s *Store) WriteFiles(files []index.FileInfo) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			_ = rbErr
		}
	}()

	// Clear existing file data
	if _, err := tx.Exec("DELETE FROM files"); err != nil {
		return fmt.Errorf("truncate files: %w", err)
	}

	// Insert new file data
	stmt, err := tx.Prepare(`INSERT INTO files (path, mtime, hash) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare files insert: %w", err)
	}
	defer stmt.Close()

	for _, f := range files {
		if _, err := stmt.Exec(f.Path, f.Mtime, f.Hash); err != nil {
			return fmt.Errorf("insert file %s: %w", f.Path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// GetAllFiles retrieves all stored file metadata for change detection.
func (s *Store) GetAllFiles() (map[string]index.FileInfo, error) {
	rows, err := s.db.Query(`SELECT path, mtime, COALESCE(hash, '') FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make(map[string]index.FileInfo)
	for rows.Next() {
		var f index.FileInfo
		if err := rows.Scan(&f.Path, &f.Mtime, &f.Hash); err != nil {
			return nil, fmt.Errorf("scan file row: %w", err)
		}
		files[f.Path] = f
	}
	return files, rows.Err()
}

// GetFileHash retrieves the content hash for a file path
func (s *Store) GetFileHash(path string) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT hash FROM files WHERE path = ?`, path).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hash, nil
}

// IncrementalResult holds stats from an incremental index write.
type IncrementalResult struct {
	OrphanedRefs     int
	IncrementalCount int // how many incremental writes since last full reindex
}

// WriteIndexIncremental updates the index for changed/deleted files only.
// Symbols for changed files are replaced; refs, call edges, and imports from
// changed files are deleted and re-inserted. Unchanged files are left in place.
// FK constraints are disabled during the write; orphaned refs are counted afterward.
func (s *Store) WriteIndexIncremental(
	changedSymbols []index.Symbol,
	newRefs []index.Ref,
	newEdges []index.CallEdge,
	newImports []index.Import,
	changedFiles []string,
	deletedFiles []string,
) (*IncrementalResult, error) {
	repoRoot, _ := s.GetMeta("repo_root")

	// Read current incremental count
	countStr, _ := s.GetMeta("incremental_count")
	incCount := 0
	if countStr != "" {
		fmt.Sscanf(countStr, "%d", &incCount) //nolint:errcheck // best-effort parse
	}
	incCount++

	conn, err := s.db.Conn(context.Background())
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()

	// Disable FK constraints (must be outside transaction in SQLite)
	if _, err := conn.ExecContext(context.Background(), "PRAGMA foreign_keys=OFF"); err != nil {
		return nil, fmt.Errorf("disable FK: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	}()

	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			_ = rbErr
		}
	}()

	// Build set of all affected files (changed + deleted)
	allAffected := make([]string, 0, len(changedFiles)+len(deletedFiles))
	allAffected = append(allAffected, changedFiles...)
	allAffected = append(allAffected, deletedFiles...)

	if len(allAffected) > 0 {
		// Delete embeddings + purposes for symbols in affected files
		if err := deleteByFilePaths(tx, "SELECT id FROM symbols", "DELETE FROM embeddings WHERE symbol_id IN", allAffected, repoRoot); err != nil {
			// Ignore if embeddings table doesn't exist
			_ = err
		}
		if err := deleteByFilePaths(tx, "SELECT id FROM symbols", "DELETE FROM symbol_purposes WHERE symbol_id IN", allAffected, repoRoot); err != nil {
			// Ignore if symbol_purposes table doesn't exist
			_ = err
		}

		// Delete data from affected files
		if err := deleteFromFileSet(tx, "refs", allAffected); err != nil {
			return nil, fmt.Errorf("delete refs: %w", err)
		}
		if err := deleteFromFileSet(tx, "call_graph", allAffected); err != nil {
			return nil, fmt.Errorf("delete call_graph: %w", err)
		}
		if err := deleteFromFileSet(tx, "imports", allAffected); err != nil {
			// Ignore if table doesn't exist
			_ = err
		}
		if err := deleteFromFileSet(tx, "symbols", allAffected); err != nil {
			return nil, fmt.Errorf("delete symbols: %w", err)
		}
	}

	// Insert new symbols for changed files
	if err := writeSymbols(tx, changedSymbols, repoRoot); err != nil {
		return nil, fmt.Errorf("write symbols: %w", err)
	}

	// Insert new refs
	if err := writeRefs(tx, newRefs, repoRoot); err != nil {
		return nil, fmt.Errorf("write refs: %w", err)
	}

	// Insert new call edges
	if err := writeCallEdges(tx, newEdges); err != nil {
		return nil, fmt.Errorf("write call edges: %w", err)
	}

	// Insert new imports
	if err := writeImports(tx, newImports); err != nil {
		return nil, fmt.Errorf("write imports: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Count orphaned refs (outside transaction)
	var orphanCount int
	if err := conn.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM refs WHERE symbol_id NOT IN (SELECT id FROM symbols)`).Scan(&orphanCount); err != nil {
		return nil, fmt.Errorf("count orphaned refs: %w", err)
	}

	// Store incremental metadata
	if _, err := conn.ExecContext(context.Background(), `INSERT OR REPLACE INTO meta (key, value) VALUES ('orphaned_refs', ?)`, fmt.Sprintf("%d", orphanCount)); err != nil {
		return nil, fmt.Errorf("set orphaned_refs: %w", err)
	}
	if _, err := conn.ExecContext(context.Background(), `INSERT OR REPLACE INTO meta (key, value) VALUES ('incremental_count', ?)`, fmt.Sprintf("%d", incCount)); err != nil {
		return nil, fmt.Errorf("set incremental_count: %w", err)
	}

	return &IncrementalResult{
		OrphanedRefs:     orphanCount,
		IncrementalCount: incCount,
	}, nil
}

// deleteFromFileSet deletes rows from a table where file_path matches any of the given paths.
func deleteFromFileSet(tx *sql.Tx, table string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	placeholders := make([]string, len(files))
	args := make([]interface{}, len(files))
	for i, f := range files {
		placeholders[i] = "?"
		args[i] = f
	}
	query := fmt.Sprintf("DELETE FROM %s WHERE file_path IN (%s)", table, strings.Join(placeholders, ",")) // #nosec G201 -- table name is hardcoded constant
	_, err := tx.Exec(query, args...)
	return err
}

// deleteByFilePaths finds symbol IDs in affected files, then deletes from a target table.
func deleteByFilePaths(tx *sql.Tx, selectQuery, deleteQuery string, files []string, repoRoot string) error {
	if len(files) == 0 {
		return nil
	}
	// Find symbol IDs in affected files (by absolute path)
	placeholders := make([]string, len(files))
	args := make([]interface{}, len(files))
	for i, f := range files {
		placeholders[i] = "?"
		args[i] = f
	}
	query := fmt.Sprintf("%s WHERE file_path IN (%s)", selectQuery, strings.Join(placeholders, ",")) // #nosec G201 -- query parts are hardcoded
	rows, err := tx.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan symbol id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(ids) == 0 {
		// Also try relative paths
		relArgs := make([]interface{}, len(files))
		for i, f := range files {
			relArgs[i] = toRelPath(f, repoRoot)
		}
		query = fmt.Sprintf("%s WHERE file_path_rel IN (%s)", selectQuery, strings.Join(placeholders, ",")) // #nosec G201 -- query parts are hardcoded
		rows2, err := tx.Query(query, relArgs...)
		if err != nil {
			return err
		}
		defer rows2.Close()
		for rows2.Next() {
			var id string
			if err := rows2.Scan(&id); err != nil {
				return fmt.Errorf("scan relative symbol id: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows2.Err(); err != nil {
			return err
		}
	}

	if len(ids) == 0 {
		return nil
	}

	// Delete from target table
	idPlaceholders := make([]string, len(ids))
	idArgs := make([]interface{}, len(ids))
	for i, id := range ids {
		idPlaceholders[i] = "?"
		idArgs[i] = id
	}
	delQuery := fmt.Sprintf("%s (%s)", deleteQuery, strings.Join(idPlaceholders, ",")) // #nosec G201 -- query parts are hardcoded
	_, err = tx.Exec(delQuery, idArgs...)
	return err
}

func deleteOptionalTable(tx *sql.Tx, table string) error {
	_, err := tx.Exec("DELETE FROM " + table)
	if err == nil {
		return nil
	}
	if isNoSuchTableErr(err, table) {
		return nil
	}
	return fmt.Errorf("truncate %s: %w", table, err)
}

func isNoSuchTableErr(err error, table string) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return strings.Contains(strings.ToLower(sqliteErr.Error()), "no such table: "+strings.ToLower(table))
}

// GetStats returns index statistics
func (s *Store) GetStats() (symbols, refs, calls int, err error) {
	err = s.db.QueryRow("SELECT COUNT(*) FROM symbols").Scan(&symbols)
	if err != nil {
		return
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM refs").Scan(&refs)
	if err != nil {
		return
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM call_graph").Scan(&calls)
	return
}
