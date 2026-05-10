package query

import (
	"database/sql"
	"fmt"

	"github.com/dkoosis/snipe/internal/output"
)

// TraceRef is one occurrence of a string literal enriched with call context.
type TraceRef struct {
	LiteralRef
	Enclosing *output.Enclosing
	Callers   []output.CallerPreview
}

// TraceString finds all occurrences of a string literal and resolves their
// enclosing functions plus immediate callers.
func TraceString(db *sql.DB, value string) ([]TraceRef, error) {
	refs, err := FindLiteralRefs(db, value)
	if err != nil {
		return nil, err
	}
	return EnrichTraceRefs(db, refs), nil
}

// EnrichTraceRefs resolves enclosing symbols and caller previews for a set of
// literal refs. Results without an enclosing_id are returned with nil Enclosing.
func EnrichTraceRefs(db *sql.DB, refs []LiteralRef) []TraceRef {
	type encInfo struct {
		sym     *SymbolRow
		callers []output.CallerPreview
	}
	cache := map[string]*encInfo{}

	resolve := func(id string) *encInfo {
		if id == "" {
			return nil
		}
		if info, ok := cache[id]; ok {
			return info
		}
		info := &encInfo{}
		if sym, err := LookupByID(db, id); err == nil {
			info.sym = sym
		}
		if info.sym != nil {
			info.callers, _ = GetCallersPreview(db, id, 5)
		}
		cache[id] = info
		return info
	}

	out := make([]TraceRef, 0, len(refs))
	for i := range refs {
		r := &refs[i]
		tr := TraceRef{LiteralRef: *r}
		if info := resolve(r.EnclosingID); info != nil && info.sym != nil {
			sym := info.sym
			enc := &output.Enclosing{
				ID:   sym.ID,
				Kind: sym.Kind,
				Name: sym.Name,
			}
			if sym.Signature.Valid {
				enc.Signature = sym.Signature.String
			}
			tr.Enclosing = enc
			tr.Callers = info.callers
		}
		out = append(out, tr)
	}
	return out
}

// FindLiteralRefsSubstring returns refs whose value contains the given substring.
// Used as a fallback when exact match returns no results.
func FindLiteralRefsSubstring(db *sql.DB, value string) ([]LiteralRef, error) {
	rows, err := db.Query(`
		SELECT id, value, COALESCE(name,''), kind, file_path, COALESCE(file_path_rel,''),
		       line, col, COALESCE(enclosing_id,''), COALESCE(snippet,'')
		FROM string_refs
		WHERE value LIKE ?
		ORDER BY file_path, line, col
		LIMIT 50
	`, fmt.Sprintf("%%%s%%", value))
	if err != nil {
		return nil, fmt.Errorf("query string_refs substring: %w", err)
	}
	defer rows.Close()

	var out []LiteralRef
	for rows.Next() {
		var r LiteralRef
		if err := rows.Scan(&r.ID, &r.Value, &r.Name, &r.Kind, &r.FilePath, &r.FilePathRel,
			&r.Line, &r.Col, &r.EnclosingID, &r.Snippet); err != nil {
			return nil, fmt.Errorf("scan string_ref: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
