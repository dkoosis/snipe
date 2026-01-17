package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dkoosis/snipe/internal/index"
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

	// Clear existing data
	if err := truncateTables(tx); err != nil {
		return err
	}

	// Write symbols
	if err := writeSymbols(tx, symbols, repoRoot); err != nil {
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
	// Delete in order: child tables first (refs, call_graph, imports), then parent (symbols)
	// This respects foreign key constraints. Using explicit statements avoids
	// string concatenation patterns that could be unsafe if copied with user input.
	if _, err := tx.Exec("DELETE FROM refs"); err != nil {
		return fmt.Errorf("truncate refs: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM call_graph"); err != nil {
		return fmt.Errorf("truncate call_graph: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM imports"); err != nil {
		// Ignore if table doesn't exist (backwards compatibility)
		_ = err
	}
	if _, err := tx.Exec("DELETE FROM symbols"); err != nil {
		return fmt.Errorf("truncate symbols: %w", err)
	}
	return nil
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

// GetFileHash retrieves the content hash for a file path
func (s *Store) GetFileHash(path string) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT hash FROM files WHERE path = ?`, path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hash, nil
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
