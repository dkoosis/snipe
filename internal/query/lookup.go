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
	ID          string
	Name        string
	Kind        string
	FilePath    string // Absolute path (for file operations)
	FilePathRel string // Relative path (for output)
	PkgPath     string // Go package path (for qualified lookups)
	LineStart   int
	ColStart    int
	LineEnd     int
	ColEnd      int
	Signature   sql.NullString
	Doc         sql.NullString
	Receiver    sql.NullString
	FileHash    string // Content hash for change detection
}

// LookupByID looks up a symbol by its ID.
func LookupByID(db *sql.DB, id string) (*SymbolRow, error) {
	var s SymbolRow
	var fileHash, filePathRel, pkgPath sql.NullString
	err := db.QueryRow(`
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE s.id = ?
	`, id).Scan(&s.ID, &s.Name, &s.Kind, &s.FilePath, &filePathRel, &pkgPath, &s.LineStart, &s.ColStart, &s.LineEnd, &s.ColEnd,
		&s.Signature, &s.Doc, &s.Receiver, &fileHash)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query symbol by id: %w", err)
	}
	s.FileHash = fileHash.String
	s.FilePathRel = filePathRel.String
	s.PkgPath = pkgPath.String
	return &s, nil
}

// BatchLookupByID looks up multiple symbols by their IDs in a single query.
// Returns a map from ID to SymbolRow. Missing IDs are not included in the map.
func BatchLookupByID(db *sql.DB, ids []string) (map[string]*SymbolRow, error) {
	if len(ids) == 0 {
		return make(map[string]*SymbolRow), nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE s.id IN (%s)
	`, strings.Join(placeholders, ", "))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("batch query symbols: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*SymbolRow, len(ids))
	for rows.Next() {
		var s SymbolRow
		var fileHash, filePathRel, pkgPath sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.FilePath, &filePathRel, &pkgPath,
			&s.LineStart, &s.ColStart, &s.LineEnd, &s.ColEnd,
			&s.Signature, &s.Doc, &s.Receiver, &fileHash); err != nil {
			return nil, fmt.Errorf("scan symbol row: %w", err)
		}
		s.FileHash = fileHash.String
		s.FilePathRel = filePathRel.String
		s.PkgPath = pkgPath.String
		result[s.ID] = &s
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbol rows: %w", err)
	}

	return result, nil
}

// LookupByName looks up symbols by name
// Returns candidates if multiple matches
func LookupByName(db *sql.DB, name string) ([]SymbolRow, error) {
	// Check for method syntax ((*T).Method or T.Method)
	if strings.HasPrefix(name, "(") || strings.Contains(name, ").") {
		return lookupMethod(db, name)
	}

	// Check for qualified name (pkg/path.Symbol or Type.Method)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		prefix := name[:idx]
		suffix := name[idx+1:]

		// If prefix looks like a type name (starts with uppercase, no slashes),
		// try method lookup first
		if len(prefix) > 0 && prefix[0] >= 'A' && prefix[0] <= 'Z' && !strings.Contains(prefix, "/") {
			results, err := lookupMethod(db, name)
			if err != nil {
				return nil, err
			}
			if len(results) > 0 {
				return results, nil
			}
			// Fall through to qualified lookup
		}

		return lookupQualified(db, prefix, suffix)
	}

	// Simple name lookup
	return lookupSimple(db, name)
}

// LookupByNameInFile looks up a symbol by name within a specific file pattern.
// The filePattern can be a full path, relative path, or partial match.
func LookupByNameInFile(db *sql.DB, name, filePattern string) ([]SymbolRow, error) {
	pattern := "%" + filePattern + "%"
	rows, err := db.Query(`
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE s.name = ? AND (s.file_path LIKE ? OR s.file_path_rel LIKE ?)
		ORDER BY s.kind, s.file_path
	`, name, pattern, pattern)
	if err != nil {
		return nil, fmt.Errorf("query symbols by name in file: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}

func lookupSimple(db *sql.DB, name string) ([]SymbolRow, error) {
	rows, err := db.Query(`
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE s.name = ?
		ORDER BY s.kind, s.file_path
	`, name)
	if err != nil {
		return nil, fmt.Errorf("query symbols by name: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}

func lookupQualified(db *sql.DB, pkgPath, name string) ([]SymbolRow, error) {
	// Use pkg_path for efficient exact or suffix matching.
	// The pkgPath may be a full path (e.g., "github.com/user/repo/internal/handler")
	// or a suffix (e.g., "internal/handler").
	//
	// First try exact match, then suffix match.
	// Uses idx_symbols_name_pkg composite index for O(log N) lookup.

	// Pattern for suffix match: pkg_path ends with /pkgPath or equals pkgPath
	suffixPattern := "%/" + pkgPath

	rows, err := db.Query(`
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE s.name = ? AND (
			s.pkg_path = ? OR
			s.pkg_path LIKE ?
		)
		ORDER BY s.kind, s.file_path
	`, name, pkgPath, suffixPattern)
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
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE s.name = ? AND (s.receiver = ? OR s.receiver = ?)
		ORDER BY s.file_path
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
		var fileHash, filePathRel, pkgPath sql.NullString
		err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.FilePath, &filePathRel, &pkgPath, &s.LineStart, &s.ColStart, &s.LineEnd, &s.ColEnd,
			&s.Signature, &s.Doc, &s.Receiver, &fileHash)
		if err != nil {
			return nil, fmt.Errorf("scan symbol row: %w", err)
		}
		s.FileHash = fileHash.String
		s.FilePathRel = filePathRel.String
		s.PkgPath = pkgPath.String
		symbols = append(symbols, s)
	}
	return symbols, rows.Err()
}

// FindRefs finds all references to a symbol
func FindRefs(db *sql.DB, symbolID string, limit, offset int) ([]RefRow, error) {
	rows, err := db.Query(`
		SELECT r.id, r.symbol_id, r.file_path, r.file_path_rel, r.line, r.col, r.enclosing_id, r.snippet,
		       s.name, s.kind, s.signature, f.hash
		FROM refs r
		LEFT JOIN symbols s ON r.enclosing_id = s.id
		LEFT JOIN files f ON r.file_path = f.path
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
		var encName, encKind, encSig, fileHash, filePathRel sql.NullString
		err := rows.Scan(&r.ID, &r.SymbolID, &r.FilePath, &filePathRel, &r.Line, &r.Col, &r.EnclosingID, &r.Snippet,
			&encName, &encKind, &encSig, &fileHash)
		if err != nil {
			return nil, fmt.Errorf("scan ref row: %w", err)
		}
		r.FilePathRel = filePathRel.String
		r.EnclosingName = encName.String
		r.EnclosingKind = encKind.String
		r.EnclosingSignature = encSig.String
		r.FileHash = fileHash.String
		refs = append(refs, r)
	}

	return refs, rows.Err()
}

// RefRow represents a reference with enclosing context
type RefRow struct {
	ID                 string
	SymbolID           string
	FilePath           string // Absolute path (for file operations)
	FilePathRel        string // Relative path (for output)
	Line               int
	Col                int
	EnclosingID        sql.NullString
	Snippet            string
	EnclosingName      string
	EnclosingKind      string
	EnclosingSignature string
	FileHash           string // Content hash for change detection
}

// ToResult converts a SymbolRow to an output.Result
func (s *SymbolRow) ToResult() output.Result {
	r := output.Range{
		Start: output.Position{Line: s.LineStart, Col: s.ColStart},
		End:   output.Position{Line: s.LineEnd, Col: s.ColEnd},
	}
	// Use relative path for output, absolute path for file operations
	filePath := s.FilePathRel
	if filePath == "" {
		filePath = s.FilePath // Fallback to absolute if relative not available
	}

	// Compute static analysis hints
	hints := s.computeHints()

	result := output.Result{
		ID:         s.ID,
		File:       filePath,
		FileAbs:    s.FilePath,
		Range:      r,
		Kind:       s.Kind,
		Name:       s.Name,
		Match:      s.Signature.String,
		Hints:      hints,
		EditTarget: output.FormatEditTargetWithHash(filePath, s.FilePath, r),
	}

	// Add receiver for methods
	if s.Receiver.Valid && s.Receiver.String != "" {
		result.Receiver = s.Receiver.String
	}

	// Add package path
	if s.PkgPath != "" {
		result.Package = s.PkgPath
	}

	return result
}

// computeHints detects static analysis hints for a symbol.
func (s *SymbolRow) computeHints() []string {
	var hints []string

	// Check for deprecated in doc string
	if s.Doc.Valid && s.Doc.String != "" {
		docLower := strings.ToLower(s.Doc.String)
		if strings.Contains(docLower, "deprecated") {
			hints = append(hints, output.HintDeprecated)
		}
	}

	// Check for pointer receiver (method can be called on nil)
	if s.Receiver.Valid && strings.HasPrefix(s.Receiver.String, "(*") {
		hints = append(hints, output.HintPointerRecv)
	}

	return hints
}

// ToResultWithHints converts a SymbolRow to an output.Result with ref count and unused detection.
// If db is provided, includes reference count and checks if exported symbols are unused.
// RefCount is set to -1 if the query fails.
func (s *SymbolRow) ToResultWithHints(db *sql.DB) output.Result {
	result := s.ToResult()

	// Get reference count (always included when db is available)
	if db != nil {
		var refCount int
		err := db.QueryRow(`SELECT COUNT(*) FROM refs WHERE symbol_id = ?`, s.ID).Scan(&refCount)
		if err != nil {
			result.RefCount = -1 // Indicate unavailable due to error
		} else {
			result.RefCount = refCount
			// Check for unused exported symbols
			if isExported(s.Name) && refCount == 0 {
				result.Hints = append(result.Hints, output.HintUnused)
			}
		}
	}

	return result
}

// GetRefCount returns the number of references to a symbol.
func GetRefCount(db *sql.DB, symbolID string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM refs WHERE symbol_id = ?`, symbolID).Scan(&count)
	return count, err
}

// isExported returns true if a Go identifier is exported (starts with uppercase).
func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}

// ToCandidate converts a SymbolRow to an output.Candidate
func (s *SymbolRow) ToCandidate() output.Candidate {
	// Use relative path for output
	filePath := s.FilePathRel
	if filePath == "" {
		filePath = s.FilePath // Fallback to absolute if relative not available
	}
	// Extract a short doc snippet (first line, truncated to 80 chars)
	docSnippet := ""
	if s.Doc.Valid && s.Doc.String != "" {
		docSnippet = s.Doc.String
		if idx := strings.Index(docSnippet, "\n"); idx != -1 {
			docSnippet = docSnippet[:idx]
		}
		if len(docSnippet) > 80 {
			docSnippet = docSnippet[:77] + "..."
		}
	}
	return output.Candidate{
		ID:   s.ID,
		Name: s.Name,
		File: filePath,
		Kind: s.Kind,
		Doc:  docSnippet,
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
	CallerFile      string // Absolute path
	CallerFileRel   string // Relative path
	CallerSignature sql.NullString
	CallerFileHash  string // Content hash for caller file
	CalleeID        string
	CalleeName      string
	CalleeKind      string
	CalleeFile      string // Absolute path
	CalleeFileRel   string // Relative path
	CalleeSignature sql.NullString
	CalleeFileHash  string // Content hash for callee file
	CallLine        int
	CallCol         int
}

// FindCallers returns all functions that call the given symbol
func FindCallers(db *sql.DB, symbolID string, limit, offset int) ([]CallRow, error) {
	rows, err := db.Query(`
		SELECT
			cg.caller_id, caller.name, caller.kind, caller.file_path, caller.file_path_rel, caller.signature, fc.hash,
			cg.callee_id, callee.name, callee.kind, callee.file_path, callee.file_path_rel, callee.signature, fe.hash,
			cg.line, cg.col
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		JOIN symbols callee ON cg.callee_id = callee.id
		LEFT JOIN files fc ON caller.file_path = fc.path
		LEFT JOIN files fe ON callee.file_path = fe.path
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
		var callerHash, calleeHash, callerFileRel, calleeFileRel sql.NullString
		err := rows.Scan(
			&r.CallerID, &r.CallerName, &r.CallerKind, &r.CallerFile, &callerFileRel, &r.CallerSignature, &callerHash,
			&r.CalleeID, &r.CalleeName, &r.CalleeKind, &r.CalleeFile, &calleeFileRel, &r.CalleeSignature, &calleeHash,
			&r.CallLine, &r.CallCol,
		)
		if err != nil {
			return nil, err
		}
		r.CallerFileRel = callerFileRel.String
		r.CalleeFileRel = calleeFileRel.String
		r.CallerFileHash = callerHash.String
		r.CalleeFileHash = calleeHash.String
		results = append(results, r)
	}
	return results, rows.Err()
}

// FindCallees returns all functions that the given symbol calls
func FindCallees(db *sql.DB, symbolID string, limit, offset int) ([]CallRow, error) {
	rows, err := db.Query(`
		SELECT
			cg.caller_id, caller.name, caller.kind, caller.file_path, caller.file_path_rel, caller.signature, fc.hash,
			cg.callee_id, callee.name, callee.kind, callee.file_path, callee.file_path_rel, callee.signature, fe.hash,
			cg.line, cg.col
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		JOIN symbols callee ON cg.callee_id = callee.id
		LEFT JOIN files fc ON caller.file_path = fc.path
		LEFT JOIN files fe ON callee.file_path = fe.path
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
		var callerHash, calleeHash, callerFileRel, calleeFileRel sql.NullString
		err := rows.Scan(
			&r.CallerID, &r.CallerName, &r.CallerKind, &r.CallerFile, &callerFileRel, &r.CallerSignature, &callerHash,
			&r.CalleeID, &r.CalleeName, &r.CalleeKind, &r.CalleeFile, &calleeFileRel, &r.CalleeSignature, &calleeHash,
			&r.CallLine, &r.CallCol,
		)
		if err != nil {
			return nil, err
		}
		r.CallerFileRel = callerFileRel.String
		r.CalleeFileRel = calleeFileRel.String
		r.CallerFileHash = callerHash.String
		r.CalleeFileHash = calleeHash.String
		results = append(results, r)
	}
	return results, rows.Err()
}

// FindImplementers finds types that potentially implement an interface.
// Since Go uses structural typing, we look for types that have methods
// matching the interface's required methods.
func FindImplementers(db *sql.DB, interfaceID string, limit, offset int) ([]SymbolRow, error) {
	// For now, we use a simpler heuristic: find struct/type symbols that reference this interface
	rows, err := db.Query(`
		SELECT DISTINCT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE s.kind IN ('struct', 'type')
		  AND EXISTS (
		    SELECT 1 FROM refs r
		    WHERE r.symbol_id = ?
		      AND r.file_path = s.file_path
		  )
		ORDER BY s.file_path, s.name
		LIMIT ? OFFSET ?
	`, interfaceID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query implementers: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}

// FindPackageSymbols finds all exported symbols in files matching a package path pattern.
// It filters to exported symbols only (those starting with uppercase).
func FindPackageSymbols(db *sql.DB, pkgPattern string, limit, offset int) ([]SymbolRow, error) {
	// Match package pattern using pkg_path for more precise matching
	// Pattern can be exact, suffix, or substring match
	suffixPattern := "%/" + pkgPattern
	substringPattern := "%" + pkgPattern + "%"

	rows, err := db.Query(`
		SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
		       s.signature, s.doc, s.receiver, f.hash
		FROM symbols s
		LEFT JOIN files f ON s.file_path = f.path
		WHERE (s.pkg_path = ? OR s.pkg_path LIKE ? OR s.pkg_path LIKE ?)
		  AND s.name GLOB '[A-Z]*'
		  AND s.kind NOT IN ('field')
		ORDER BY s.kind, s.name
		LIMIT ? OFFSET ?
	`, pkgPattern, suffixPattern, substringPattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query package symbols: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}

// GetCallersPreview returns a preview of top N callers for a symbol.
// Used for quick caller context without full call graph traversal.
func GetCallersPreview(db *sql.DB, symbolID string, limit int) ([]output.CallerPreview, error) {
	rows, err := db.Query(`
		SELECT caller.id, caller.name, caller.file_path_rel, cg.line
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		WHERE cg.callee_id = ?
		ORDER BY caller.file_path_rel, cg.line
		LIMIT ?
	`, symbolID, limit)
	if err != nil {
		return nil, fmt.Errorf("query callers preview: %w", err)
	}
	defer rows.Close()

	var callers []output.CallerPreview
	for rows.Next() {
		var c output.CallerPreview
		var fileRel sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &fileRel, &c.Line); err != nil {
			return nil, fmt.Errorf("scan caller preview: %w", err)
		}
		c.File = fileRel.String
		callers = append(callers, c)
	}
	return callers, rows.Err()
}

// FindSymbolsByKind finds symbols of a specific kind, optionally filtered by file path pattern.
func FindSymbolsByKind(db *sql.DB, kind string, filePattern string, limit, offset int) ([]SymbolRow, error) {
	var rows *sql.Rows
	var err error

	if filePattern != "" {
		pattern := "%" + filePattern + "%"
		rows, err = db.Query(`
			SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
			       s.signature, s.doc, s.receiver, f.hash
			FROM symbols s
			LEFT JOIN files f ON s.file_path = f.path
			WHERE s.kind = ?
			  AND (s.pkg_path LIKE ? OR s.file_path_rel LIKE ? OR s.file_path LIKE ?)
			ORDER BY s.file_path, s.line_start
			LIMIT ? OFFSET ?
		`, kind, pattern, pattern, pattern, limit, offset)
	} else {
		rows, err = db.Query(`
			SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel, s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
			       s.signature, s.doc, s.receiver, f.hash
			FROM symbols s
			LEFT JOIN files f ON s.file_path = f.path
			WHERE s.kind = ?
			ORDER BY s.file_path, s.line_start
			LIMIT ? OFFSET ?
		`, kind, limit, offset)
	}

	if err != nil {
		return nil, fmt.Errorf("query symbols by kind: %w", err)
	}
	defer rows.Close()

	return scanSymbolRows(rows)
}
