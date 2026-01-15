package query

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/dkoosis/snipe/internal/output"
)

// TODO: Add context.Context to all query functions for cancellation support.
// This is a future improvement - see https://github.com/dkoosis/snipe/issues/XX

// SymbolRow represents a row from the symbols table
type SymbolRow struct {
	ID        string
	Name      string
	Kind      string
	FilePath  string
	LineStart int
	ColStart  int
	LineEnd   int
	ColEnd    int
	Signature sql.NullString
	Doc       sql.NullString
	Receiver  sql.NullString
}

// LookupByID looks up a symbol by its ID.
func LookupByID(db *sql.DB, id string) (*SymbolRow, error) {
	var s SymbolRow
	err := db.QueryRow(`
		SELECT id, name, kind, file_path, line_start, col_start, line_end, col_end, signature, doc, receiver
		FROM symbols WHERE id = ?
	`, id).Scan(&s.ID, &s.Name, &s.Kind, &s.FilePath, &s.LineStart, &s.ColStart, &s.LineEnd, &s.ColEnd, &s.Signature, &s.Doc, &s.Receiver)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query symbol by id: %w", err)
	}
	return &s, nil
}

// LookupByName looks up symbols by name
// Returns candidates if multiple matches
func LookupByName(db *sql.DB, name string) ([]SymbolRow, error) {
	// Check for qualified name (pkg/path.Symbol)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return lookupQualified(db, name[:idx], name[idx+1:])
	}

	// Check for method syntax ((*T).Method or T.Method)
	if strings.HasPrefix(name, "(") || strings.Contains(name, ").") {
		return lookupMethod(db, name)
	}

	// Simple name lookup
	return lookupSimple(db, name)
}

func lookupSimple(db *sql.DB, name string) ([]SymbolRow, error) {
	rows, err := db.Query(`
		SELECT id, name, kind, file_path, line_start, col_start, line_end, col_end, signature, doc, receiver
		FROM symbols WHERE name = ?
		ORDER BY kind, file_path
	`, name)
	if err != nil {
		return nil, fmt.Errorf("query symbols by name: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}

func lookupQualified(db *sql.DB, pkgPath, name string) ([]SymbolRow, error) {
	rows, err := db.Query(`
		SELECT id, name, kind, file_path, line_start, col_start, line_end, col_end, signature, doc, receiver
		FROM symbols WHERE name = ? AND file_path LIKE ?
		ORDER BY kind, file_path
	`, name, "%"+pkgPath+"%")
	if err != nil {
		return nil, fmt.Errorf("query symbols qualified: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}

func lookupMethod(db *sql.DB, name string) ([]SymbolRow, error) {
	// Parse method syntax: (*T).Method or (T).Method
	var receiver, method string

	if idx := strings.Index(name, ")."); idx >= 0 {
		receiver = name[:idx+1]
		method = name[idx+2:]
	} else {
		// Try T.Method format
		parts := strings.SplitN(name, ".", 2)
		if len(parts) == 2 {
			receiver = "(" + parts[0] + ")"
			method = parts[1]
		} else {
			return nil, nil
		}
	}

	rows, err := db.Query(`
		SELECT id, name, kind, file_path, line_start, col_start, line_end, col_end, signature, doc, receiver
		FROM symbols WHERE name = ? AND (receiver = ? OR receiver = ?)
		ORDER BY file_path
	`, method, receiver, "(*"+strings.Trim(receiver, "()")+")")
	if err != nil {
		return nil, fmt.Errorf("query method: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}

func scanSymbolRows(rows *sql.Rows) ([]SymbolRow, error) {
	var symbols []SymbolRow
	for rows.Next() {
		var s SymbolRow
		err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.FilePath, &s.LineStart, &s.ColStart, &s.LineEnd, &s.ColEnd, &s.Signature, &s.Doc, &s.Receiver)
		if err != nil {
			return nil, fmt.Errorf("scan symbol row: %w", err)
		}
		symbols = append(symbols, s)
	}
	return symbols, rows.Err()
}

// FindRefs finds all references to a symbol
func FindRefs(db *sql.DB, symbolID string, limit, offset int) ([]RefRow, error) {
	rows, err := db.Query(`
		SELECT r.id, r.symbol_id, r.file_path, r.line, r.col, r.enclosing_id, r.snippet,
		       s.name, s.kind, s.signature
		FROM refs r
		LEFT JOIN symbols s ON r.enclosing_id = s.id
		WHERE r.symbol_id = ?
		ORDER BY r.file_path, r.line
		LIMIT ? OFFSET ?
	`, symbolID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query refs: %w", err)
	}
	defer rows.Close()

	var refs []RefRow
	for rows.Next() {
		var r RefRow
		var encName, encKind, encSig sql.NullString
		err := rows.Scan(&r.ID, &r.SymbolID, &r.FilePath, &r.Line, &r.Col, &r.EnclosingID, &r.Snippet,
			&encName, &encKind, &encSig)
		if err != nil {
			return nil, fmt.Errorf("scan ref row: %w", err)
		}
		r.EnclosingName = encName.String
		r.EnclosingKind = encKind.String
		r.EnclosingSignature = encSig.String
		refs = append(refs, r)
	}

	return refs, rows.Err()
}

// RefRow represents a reference with enclosing context
type RefRow struct {
	ID                 string
	SymbolID           string
	FilePath           string
	Line               int
	Col                int
	EnclosingID        sql.NullString
	Snippet            string
	EnclosingName      string
	EnclosingKind      string
	EnclosingSignature string
}

// ToResult converts a SymbolRow to an output.Result
func (s *SymbolRow) ToResult() output.Result {
	return output.Result{
		ID:   s.ID,
		File: s.FilePath,
		Range: output.Range{
			Start: output.Position{Line: s.LineStart, Col: s.ColStart},
			End:   output.Position{Line: s.LineEnd, Col: s.ColEnd},
		},
		Kind:  s.Kind,
		Name:  s.Name,
		Match: s.Signature.String,
		EditTarget: output.FormatEditTarget(s.FilePath, output.Range{
			Start: output.Position{Line: s.LineStart, Col: s.ColStart},
			End:   output.Position{Line: s.LineEnd, Col: s.ColEnd},
		}),
	}
}

// ToCandidate converts a SymbolRow to an output.Candidate
func (s *SymbolRow) ToCandidate() output.Candidate {
	return output.Candidate{
		ID:   s.ID,
		Name: s.Name,
		File: s.FilePath,
		Kind: s.Kind,
	}
}

// FindSiblings finds other symbols of the same kind in the same file
func FindSiblings(db *sql.DB, filePath, kind, excludeID string, limit int) ([]output.Sibling, error) {
	rows, err := db.Query(`
		SELECT id, name, kind, line_start
		FROM symbols
		WHERE file_path = ? AND kind = ? AND id != ?
		ORDER BY line_start
		LIMIT ?
	`, filePath, kind, excludeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var siblings []output.Sibling
	for rows.Next() {
		var s output.Sibling
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.Line); err != nil {
			return nil, err
		}
		siblings = append(siblings, s)
	}
	return siblings, rows.Err()
}

// CallRow represents a call graph edge with caller/callee details
type CallRow struct {
	CallerID        string
	CallerName      string
	CallerKind      string
	CallerFile      string
	CallerSignature sql.NullString
	CalleeID        string
	CalleeName      string
	CalleeKind      string
	CalleeFile      string
	CalleeSignature sql.NullString
	CallLine        int
	CallCol         int
}

// FindCallers returns all functions that call the given symbol
func FindCallers(db *sql.DB, symbolID string, limit, offset int) ([]CallRow, error) {
	rows, err := db.Query(`
		SELECT
			cg.caller_id, caller.name, caller.kind, caller.file_path, caller.signature,
			cg.callee_id, callee.name, callee.kind, callee.file_path, callee.signature,
			cg.line, cg.col
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		JOIN symbols callee ON cg.callee_id = callee.id
		WHERE cg.callee_id = ?
		ORDER BY caller.file_path, cg.line
		LIMIT ? OFFSET ?
	`, symbolID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CallRow
	for rows.Next() {
		var r CallRow
		err := rows.Scan(
			&r.CallerID, &r.CallerName, &r.CallerKind, &r.CallerFile, &r.CallerSignature,
			&r.CalleeID, &r.CalleeName, &r.CalleeKind, &r.CalleeFile, &r.CalleeSignature,
			&r.CallLine, &r.CallCol,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// FindCallees returns all functions that the given symbol calls
func FindCallees(db *sql.DB, symbolID string, limit, offset int) ([]CallRow, error) {
	rows, err := db.Query(`
		SELECT
			cg.caller_id, caller.name, caller.kind, caller.file_path, caller.signature,
			cg.callee_id, callee.name, callee.kind, callee.file_path, callee.signature,
			cg.line, cg.col
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		JOIN symbols callee ON cg.callee_id = callee.id
		WHERE cg.caller_id = ?
		ORDER BY cg.line, cg.col
		LIMIT ? OFFSET ?
	`, symbolID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CallRow
	for rows.Next() {
		var r CallRow
		err := rows.Scan(
			&r.CallerID, &r.CallerName, &r.CallerKind, &r.CallerFile, &r.CallerSignature,
			&r.CalleeID, &r.CalleeName, &r.CalleeKind, &r.CalleeFile, &r.CalleeSignature,
			&r.CallLine, &r.CallCol,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
