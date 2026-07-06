package query

import (
	"database/sql"
	"fmt"
	"strings"
)

// TypeInfo contains type relationship information.
type TypeInfo struct {
	Symbol  SymbolRow
	Methods []MethodInfo
	Embeds  []EmbedInfo
	Fields  []FieldInfo
	// Implements is marked as partial - full interface satisfaction
	// requires type-checker analysis which is a v2 feature.
	Implements ImplementsInfo
}

// MethodInfo describes a method on a type.
type MethodInfo struct {
	ID        string
	Name      string
	Signature string
	Receiver  string
	File      string
	Line      int
	Doc       string
}

// EmbedInfo describes an embedded type.
type EmbedInfo struct {
	ID        string
	TypeName  string
	FieldName string // Anonymous if empty
	File      string
	Line      int
}

// FieldInfo describes a struct field.
type FieldInfo struct {
	Name     string
	TypeExpr string
	Tag      string
	Line     int
}

// ImplementsInfo tracks interface implementation status.
// V1: This is always partial as we don't have full type-checking.
type ImplementsInfo struct {
	Status     string   `json:"status"` // "partial" or "unknown"
	Interfaces []string `json:"interfaces,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// GetTypeInfo retrieves type information for a symbol by ID.
// It performs a LookupByID then delegates to GetTypeInfoFromSymbol. Callers
// that already hold the *SymbolRow (e.g. from a prior FindPackageSymbols/
// LookupByName call) should call GetTypeInfoFromSymbol directly to avoid the
// redundant lookup.
func GetTypeInfo(db *sql.DB, symbolID string) (*TypeInfo, error) {
	sym, err := LookupByID(db, symbolID)
	if err != nil {
		return nil, err
	}
	if sym == nil {
		return nil, fmt.Errorf("symbol not found: %s", symbolID)
	}

	return GetTypeInfoFromSymbol(db, sym)
}

// GetTypeInfoFromSymbol retrieves type information for an already-loaded
// symbol row, skipping the LookupByID that GetTypeInfo performs. Use this
// when the *SymbolRow is already in hand (e.g. from FindPackageSymbols or
// LookupByName) to avoid an N+1 lookup pattern.
func GetTypeInfoFromSymbol(db *sql.DB, sym *SymbolRow) (*TypeInfo, error) {
	if sym == nil {
		return nil, fmt.Errorf("symbol is nil")
	}

	// Only struct, interface, type alias kinds are valid
	if sym.Kind != kindStruct && sym.Kind != "interface" && sym.Kind != "type" {
		return nil, fmt.Errorf("types command requires struct/interface/type, got %s", sym.Kind)
	}

	info := &TypeInfo{
		Symbol: *sym,
		Implements: ImplementsInfo{
			Status: "partial",
			Note:   "Full interface satisfaction requires type-checker (v2 feature)",
		},
	}

	// Get methods
	methods, err := GetMethodsForType(db, sym.Name, sym.PkgPath)
	if err != nil {
		return nil, fmt.Errorf("get methods for %s: %w", sym.Name, err)
	}
	info.Methods = methods

	// Get embeds (from refs where the reference is in this type's definition)
	embeds, err := getEmbedsForType(db, sym.ID, sym.FilePath, sym.LineStart, sym.LineEnd)
	if err != nil {
		return nil, fmt.Errorf("get embeds for %s: %w", sym.Name, err)
	}
	info.Embeds = embeds

	// Get fields (from symbols with kind=field in this type)
	fields, err := getFieldsForType(db, sym.ID, sym.FilePath, sym.LineStart, sym.LineEnd)
	if err != nil {
		return nil, fmt.Errorf("get fields for %s: %w", sym.Name, err)
	}
	info.Fields = fields

	return info, nil
}

// GetMethodsForType finds all methods for a type by receiver matching.
func GetMethodsForType(db *sql.DB, typeName, _ string) ([]MethodInfo, error) {
	// Match both value and pointer receivers
	valueRecv := "(" + typeName + ")"
	ptrRecv := "(*" + typeName + ")"

	rows, err := db.Query(`
		SELECT id, name, signature, receiver, file_path_rel, line_start, doc
		FROM symbols
		WHERE kind = 'method'
		  AND (receiver = ? OR receiver = ?)
		  AND name GLOB '[A-Z]*'
		ORDER BY name
	`, valueRecv, ptrRecv)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var methods []MethodInfo
	for rows.Next() {
		var m MethodInfo
		var sig, recv, doc sql.NullString
		if err := rows.Scan(&m.ID, &m.Name, &sig, &recv, &m.File, &m.Line, &doc); err != nil {
			return nil, err
		}
		m.Signature = sig.String
		m.Receiver = recv.String
		m.Doc = doc.String
		methods = append(methods, m)
	}
	return methods, rows.Err()
}

// getEmbedsForType finds embedded types within a struct definition.
// Uses snippet heuristic: a ref is a true embed when its trimmed snippet starts
// with the type name (or *TypeName / pkg.TypeName), meaning no prior field name.
// Named fields like "Definition *Result" start with the field name, not the type.
func getEmbedsForType(db *sql.DB, _, filePath string, lineStart, lineEnd int) ([]EmbedInfo, error) {
	rows, err := db.Query(`
		SELECT r.symbol_id, s.name, s.kind, r.file_path_rel, r.line, r.snippet
		FROM refs r
		JOIN symbols s ON r.symbol_id = s.id
		WHERE r.file_path = ?
		  AND r.line > ? AND r.line < ?
		  AND s.kind IN ('struct', 'interface', 'type')
		ORDER BY r.line
	`, filePath, lineStart, lineEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]bool{}
	var embeds []EmbedInfo
	for rows.Next() {
		var e EmbedInfo
		var kind, snippet string
		if err := rows.Scan(&e.ID, &e.TypeName, &kind, &e.File, &e.Line, &snippet); err != nil {
			return nil, err
		}
		if !isEmbedSnippet(snippet, e.TypeName) {
			continue
		}
		if seen[e.TypeName] {
			continue
		}
		seen[e.TypeName] = true
		embeds = append(embeds, e)
	}
	return embeds, rows.Err()
}

// isEmbedSnippet reports whether a ref snippet looks like an embedded field
// rather than a named field.
// A true Go embed has exactly one token in the line (the type, possibly *-prefixed
// and package-qualified): e.g. "LiteralRef" or "*output.Enclosing".
// A named field always has two+ tokens: "Enclosing *output.Enclosing".
// We ignore trailing comments (tokens starting with "//").
func isEmbedSnippet(snippet, typeName string) bool {
	tokens := strings.Fields(strings.TrimSpace(snippet))
	if len(tokens) == 0 {
		return false
	}
	// Discard trailing comment tokens.
	for len(tokens) > 0 && strings.HasPrefix(tokens[len(tokens)-1], "//") {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) != 1 {
		return false
	}
	// The single token must end with the type name (after stripping * and pkg prefix).
	tok := strings.TrimPrefix(tokens[0], "*")
	if i := strings.LastIndex(tok, "."); i >= 0 {
		tok = tok[i+1:]
	}
	tok = strings.TrimRight(tok, "`{")
	return tok == typeName
}

// getFieldsForType finds fields within a struct definition.
func getFieldsForType(db *sql.DB, _, filePath string, lineStart, lineEnd int) ([]FieldInfo, error) {
	// Query field symbols within the struct's line range
	rows, err := db.Query(`
		SELECT name, signature, line_start
		FROM symbols
		WHERE file_path = ?
		  AND kind = 'field'
		  AND line_start > ? AND line_start < ?
		ORDER BY line_start
	`, filePath, lineStart, lineEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fields []FieldInfo
	for rows.Next() {
		var f FieldInfo
		var sig sql.NullString
		if err := rows.Scan(&f.Name, &sig, &f.Line); err != nil {
			return nil, err
		}
		f.TypeExpr = sig.String
		fields = append(fields, f)
	}
	return fields, rows.Err()
}
