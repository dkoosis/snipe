package embed

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/store"
)

type mockEmbedder struct {
	vec []float32
	err error
}

func (m *mockEmbedder) EmbedOne(_ context.Context, _, _ string) ([]float32, error) {
	return m.vec, m.err
}

func openSearchTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func writeSymbols(t *testing.T, s *store.Store, syms []index.Symbol) {
	t.Helper()
	if err := s.WriteIndex(syms, nil, nil); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}
}

func TestSearch_NoEmbeddings_ReturnsNil(t *testing.T) {
	s := openSearchTestStore(t)
	results, dur, err := Search(context.Background(), "query", s, &mockEmbedder{vec: []float32{1, 0, 0}}, 10, 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %d", len(results))
	}
	_ = dur
}

func TestSearch_ThresholdFiltersAndSortsByScore(t *testing.T) {
	s := openSearchTestStore(t)
	writeSymbols(t, s, []index.Symbol{
		{ID: "sym1", Name: "HighSim", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 1, ColStart: 1, LineEnd: 5, ColEnd: 1, Signature: "func HighSim()"},
		{ID: "sym2", Name: "LowSim", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 10, ColStart: 1, LineEnd: 15, ColEnd: 1, Signature: "func LowSim()"},
	})
	// sym1: [1,0,0] — sim=1.0 with query [1,0,0]; sym2: [0,1,0] — sim=0.0
	if err := s.SaveEmbedding("sym1", []float32{1, 0, 0}, "test"); err != nil {
		t.Fatalf("SaveEmbedding sym1: %v", err)
	}
	if err := s.SaveEmbedding("sym2", []float32{0, 1, 0}, "test"); err != nil {
		t.Fatalf("SaveEmbedding sym2: %v", err)
	}

	results, dur, err := Search(context.Background(), "query", s, &mockEmbedder{vec: []float32{1, 0, 0}}, 10, 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result above threshold 0.5, got %d", len(results))
	}
	if results[0].Name != "HighSim" {
		t.Fatalf("expected HighSim, got %s", results[0].Name)
	}
	if dur <= 0 {
		t.Fatal("expected duration > 0")
	}
}

// TestSearch_TopResultCarriesCosineScore runtime-confirms the input to the
// `! semantic:N` marker (snipe-ffj): a fallback hit carries its cosine
// similarity on Result.Score, top result first. This score is dropped from
// default Claude output, which is why the marker re-surfaces it.
func TestSearch_TopResultCarriesCosineScore(t *testing.T) {
	s := openSearchTestStore(t)
	writeSymbols(t, s, []index.Symbol{
		{ID: "sym1", Name: "Exact", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 1, ColStart: 1, LineEnd: 5, ColEnd: 1, Signature: "func Exact()"},
		{ID: "sym2", Name: "Near", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 10, ColStart: 1, LineEnd: 15, ColEnd: 1, Signature: "func Near()"},
	})
	// sym1 == query → sim 1.0; sym2 at 45° → sim ~0.707.
	if err := s.SaveEmbedding("sym1", []float32{1, 0, 0}, "test"); err != nil {
		t.Fatalf("SaveEmbedding sym1: %v", err)
	}
	if err := s.SaveEmbedding("sym2", []float32{1, 1, 0}, "test"); err != nil {
		t.Fatalf("SaveEmbedding sym2: %v", err)
	}

	results, _, err := Search(context.Background(), "query", s, &mockEmbedder{vec: []float32{1, 0, 0}}, 10, 0.3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Top hit first, score populated and in descending order.
	if results[0].Score < results[1].Score {
		t.Errorf("results not sorted by score desc: %v < %v", results[0].Score, results[1].Score)
	}
	if results[0].Score < 0.99 {
		t.Errorf("top hit: want cosine ~1.0, got %v", results[0].Score)
	}
}

func TestSearch_RespectsLimit(t *testing.T) {
	s := openSearchTestStore(t)
	writeSymbols(t, s, []index.Symbol{
		{ID: "sym1", Name: "A", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 1, ColStart: 1, LineEnd: 2, ColEnd: 1},
		{ID: "sym2", Name: "B", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 3, ColStart: 1, LineEnd: 4, ColEnd: 1},
		{ID: "sym3", Name: "C", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 5, ColStart: 1, LineEnd: 6, ColEnd: 1},
	})
	for _, id := range []string{"sym1", "sym2", "sym3"} {
		if err := s.SaveEmbedding(id, []float32{1, 0, 0}, "test"); err != nil {
			t.Fatalf("SaveEmbedding %s: %v", id, err)
		}
	}

	results, _, err := Search(context.Background(), "query", s, &mockEmbedder{vec: []float32{1, 0, 0}}, 2, 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (limit=2), got %d", len(results))
	}
}

func TestSearch_EmbedderError_ReturnsError(t *testing.T) {
	s := openSearchTestStore(t)
	writeSymbols(t, s, []index.Symbol{
		{ID: "sym1", Name: "F", Kind: index.KindFunc, FilePath: "/t.go", LineStart: 1, ColStart: 1, LineEnd: 2, ColEnd: 1},
	})
	if err := s.SaveEmbedding("sym1", []float32{1, 0, 0}, "test"); err != nil {
		t.Fatalf("SaveEmbedding: %v", err)
	}

	emb := &mockEmbedder{err: errTest}
	_, _, err := Search(context.Background(), "query", s, emb, 10, 0.5)
	if err == nil {
		t.Fatal("expected error from embedder, got nil")
	}
}

// errTest is a sentinel error for mock use.
var errTest = &sentinelError{"embed failed"}

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }
