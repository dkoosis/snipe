package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

// TestGetEmbeddingCorruptBlob verifies that a non-empty embedding BLOB that
// cannot be deserialized (length not a multiple of 4 bytes) surfaces
// ErrCorruptEmbedding instead of the silent (nil, model, nil) that lets a
// corrupt vector score 0 and never match.
func TestGetEmbeddingCorruptBlob(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// The embeddings table has a FK on symbol_id, so a symbol must exist first.
	sym := index.Symbol{
		ID: "corrupt-sym", Name: "Func1", Kind: index.KindFunc,
		FilePath: "/a.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1,
	}
	if err := s.WriteIndex([]index.Symbol{sym}, nil, nil); err != nil {
		t.Fatalf("WriteIndex failed: %v", err)
	}

	// Store an odd-length BLOB directly. DeserializeEmbedding returns nil for
	// any length that is not a multiple of 4 bytes — the corruption case.
	corrupt := []byte{0x01, 0x02, 0x03}
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO embeddings (symbol_id, embedding, model, created_at) VALUES (?, ?, ?, ?)`,
		sym.ID, corrupt, "test-model", "2026-06-27T00:00:00Z",
	); err != nil {
		t.Fatalf("insert corrupt embedding: %v", err)
	}

	emb, model, err := s.GetEmbedding(sym.ID)
	if err == nil {
		t.Fatalf("expected corruption error for odd-length blob, got nil (emb=%v model=%q)", emb, model)
	}
	if !errors.Is(err, ErrCorruptEmbedding) {
		t.Errorf("expected ErrCorruptEmbedding, got %v", err)
	}
	if emb != nil {
		t.Errorf("expected nil embedding on corruption, got %v", emb)
	}
}

// TestGetEmbeddingValidRoundTrip guards the happy path: a valid stored
// embedding still round-trips without tripping the corruption check.
func TestGetEmbeddingValidRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	sym := index.Symbol{
		ID: "good-sym", Name: "Func1", Kind: index.KindFunc,
		FilePath: "/a.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1,
	}
	if err := s.WriteIndex([]index.Symbol{sym}, nil, nil); err != nil {
		t.Fatalf("WriteIndex failed: %v", err)
	}

	want := []float32{0.1, 0.2, 0.3}
	if err := s.SaveEmbedding(sym.ID, want, "test-model"); err != nil {
		t.Fatalf("SaveEmbedding failed: %v", err)
	}

	emb, model, err := s.GetEmbedding(sym.ID)
	if err != nil {
		t.Fatalf("GetEmbedding failed: %v", err)
	}
	if model != "test-model" {
		t.Errorf("model = %q, want test-model", model)
	}
	if len(emb) != len(want) {
		t.Fatalf("embedding len = %d, want %d", len(emb), len(want))
	}
	for i := range want {
		if emb[i] != want[i] {
			t.Errorf("embedding[%d] = %v, want %v", i, emb[i], want[i])
		}
	}
}
