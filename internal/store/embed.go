package store

import (
	"errors"
	"fmt"
	"time"

	"github.com/dkoosis/snipe/internal/vector"
)

// ErrCorruptEmbedding signals that a stored embedding BLOB could not be
// deserialized — its length is not a multiple of 4 bytes. Callers must treat
// this distinctly from an absent embedding (sql.ErrNoRows) or a valid result,
// rather than silently scoring a corrupt vector as 0.
var ErrCorruptEmbedding = errors.New("corrupt embedding blob")

// SaveEmbedding stores an embedding for a symbol.
func (s *Store) SaveEmbedding(symbolID string, embedding []float32, model string) error {
	data := vector.SerializeEmbedding(embedding)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO embeddings (symbol_id, embedding, model, created_at) VALUES (?, ?, ?, ?)`,
		symbolID, data, model, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("save embedding for symbol %s: %w", symbolID, err)
	}
	return nil
}

// GetEmbedding retrieves an embedding for a symbol.
func (s *Store) GetEmbedding(symbolID string) ([]float32, string, error) {
	var data []byte
	var model string
	err := s.db.QueryRow(
		`SELECT embedding, model FROM embeddings WHERE symbol_id = ?`,
		symbolID,
	).Scan(&data, &model)
	if err != nil {
		return nil, "", fmt.Errorf("get embedding for symbol %s: %w", symbolID, err)
	}
	emb := vector.DeserializeEmbedding(data)
	if emb == nil && len(data) > 0 {
		// Non-empty BLOB that failed to deserialize (odd/malformed length):
		// surface corruption instead of returning (nil, model, nil), which a
		// caller would otherwise mistake for a valid empty vector.
		return nil, "", fmt.Errorf("get embedding for symbol %s: %w", symbolID, ErrCorruptEmbedding)
	}
	return emb, model, nil
}

// EmbeddingRow represents a row from the embeddings table with symbol info.
type EmbeddingRow struct {
	SymbolID  string
	Embedding []float32
	Model     string
	Name      string
	Kind      string
	FilePath  string
	Signature string
}

// GetAllEmbeddings retrieves all embeddings with their symbol info.
func (s *Store) GetAllEmbeddings() ([]EmbeddingRow, error) {
	rows, err := s.db.Query(`
		SELECT e.symbol_id, e.embedding, e.model, s.name, s.kind, s.file_path, COALESCE(s.signature, '')
		FROM embeddings e
		JOIN symbols s ON e.symbol_id = s.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	var results []EmbeddingRow
	for rows.Next() {
		var r EmbeddingRow
		var data []byte
		if err := rows.Scan(&r.SymbolID, &data, &r.Model, &r.Name, &r.Kind, &r.FilePath, &r.Signature); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		r.Embedding = vector.DeserializeEmbedding(data)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embeddings: %w", err)
	}

	return results, nil
}

// PkgEmbeddingRow is one symbol embedding with package and signature, used by
// pair-scan callers that need to group by pkg_path.
type PkgEmbeddingRow struct {
	SymbolID  string
	Pkg       string
	Name      string
	Kind      string
	FilePath  string
	LineStart int
	Signature string
	Embedding []float32
}

// GetEmbeddingsByPackage returns embeddings restricted to function/method symbols
// (the only kinds where same-pkg pair scans make sense) joined with pkg_path.
func (s *Store) GetEmbeddingsByPackage() ([]PkgEmbeddingRow, error) {
	rows, err := s.db.Query(`
		SELECT e.symbol_id, s.pkg_path, s.name, s.kind, s.file_path, s.line_start,
		       COALESCE(s.signature, ''), e.embedding
		FROM embeddings e
		JOIN symbols s ON e.symbol_id = s.id
		WHERE s.kind IN ('func','method')
		  AND s.file_path NOT LIKE '%_test.go'
	`)
	if err != nil {
		return nil, fmt.Errorf("query embeddings by pkg: %w", err)
	}
	defer rows.Close()

	var out []PkgEmbeddingRow
	for rows.Next() {
		var r PkgEmbeddingRow
		var data []byte
		if err := rows.Scan(&r.SymbolID, &r.Pkg, &r.Name, &r.Kind, &r.FilePath, &r.LineStart, &r.Signature, &data); err != nil {
			return nil, fmt.Errorf("scan pkg embedding row: %w", err)
		}
		r.Embedding = vector.DeserializeEmbedding(data)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pkg embeddings: %w", err)
	}
	return out, nil
}

// GetCalleesForSymbols returns a map from caller symbol_id to the set of its
// distinct callee_ids. One query covers all requested IDs.
func (s *Store) GetCalleesForSymbols(symbolIDs []string) (map[string]map[string]struct{}, error) {
	if len(symbolIDs) == 0 {
		return map[string]map[string]struct{}{}, nil
	}
	placeholders := make([]byte, 0, len(symbolIDs)*2)
	args := make([]interface{}, 0, len(symbolIDs))
	for i, id := range symbolIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	q := `SELECT caller_id, callee_id FROM call_graph WHERE caller_id IN (` + string(placeholders) + `)`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query callees: %w", err)
	}
	defer rows.Close()

	out := make(map[string]map[string]struct{}, len(symbolIDs))
	for rows.Next() {
		var caller, callee string
		if err := rows.Scan(&caller, &callee); err != nil {
			return nil, fmt.Errorf("scan callees row: %w", err)
		}
		set, ok := out[caller]
		if !ok {
			set = make(map[string]struct{})
			out[caller] = set
		}
		set[callee] = struct{}{}
	}
	return out, rows.Err()
}

// CountEmbeddings returns the number of stored embeddings.
func (s *Store) CountEmbeddings() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return count, nil
}
