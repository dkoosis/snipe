package store

import (
	"database/sql"
	"fmt"

	"github.com/dkoosis/snipe/internal/index"
)

// WriteIndex writes symbols, refs, and call edges to the database
// This performs a full reindex (truncate + insert)
func (s *Store) WriteIndex(symbols []index.Symbol, refs []index.Ref, edges []index.CallEdge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear existing data
	if err := truncateTables(tx); err != nil {
		return err
	}

	// Write symbols
	if err := writeSymbols(tx, symbols); err != nil {
		return err
	}

	// Write refs
	if err := writeRefs(tx, refs); err != nil {
		return err
	}

	// Write call edges
	if err := writeCallEdges(tx, edges); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func truncateTables(tx *sql.Tx) error {
	tables := []string{"symbols", "refs", "call_graph"}
	for _, table := range tables {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("truncate %s: %w", table, err)
		}
	}
	return nil
}

func writeSymbols(tx *sql.Tx, symbols []index.Symbol) error {
	stmt, err := tx.Prepare(`
		INSERT INTO symbols (id, name, kind, file_path, line_start, col_start, line_end, col_end, signature, doc, receiver)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare symbols insert: %w", err)
	}
	defer stmt.Close()

	for _, sym := range symbols {
		_, err := stmt.Exec(
			sym.ID,
			sym.Name,
			string(sym.Kind),
			sym.FilePath,
			sym.LineStart,
			sym.ColStart,
			sym.LineEnd,
			sym.ColEnd,
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

func writeRefs(tx *sql.Tx, refs []index.Ref) error {
	stmt, err := tx.Prepare(`
		INSERT INTO refs (id, symbol_id, file_path, line, col, enclosing_id, snippet)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare refs insert: %w", err)
	}
	defer stmt.Close()

	for _, ref := range refs {
		_, err := stmt.Exec(
			ref.ID,
			ref.SymbolID,
			ref.FilePath,
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
