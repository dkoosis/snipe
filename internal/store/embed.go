package store

import (
	"fmt"
	"time"

	"github.com/dkoosis/snipe/internal/vector"
)

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
	return vector.DeserializeEmbedding(data), model, nil
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

// CountEmbeddings returns the number of stored embeddings.
func (s *Store) CountEmbeddings() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return count, nil
}
