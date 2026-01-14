package store

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

func TestWriteIndex(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	symbols := []index.Symbol{
		{
			ID:        "sym1",
			Name:      "TestFunc",
			Kind:      index.KindFunc,
			FilePath:  "/test/main.go",
			LineStart: 10,
			ColStart:  1,
			LineEnd:   20,
			ColEnd:    1,
			Signature: "func TestFunc()",
			Doc:       "Test function",
		},
		{
			ID:        "sym2",
			Name:      "TestType",
			Kind:      index.KindStruct,
			FilePath:  "/test/types.go",
			LineStart: 5,
			ColStart:  6,
			LineEnd:   15,
			ColEnd:    1,
		},
	}

	refs := []index.Ref{
		{
			ID:          "ref1",
			SymbolID:    "sym1",
			FilePath:    "/test/other.go",
			Line:        30,
			Col:         10,
			EnclosingID: "sym2",
			Snippet:     "TestFunc()",
		},
	}

	edges := []index.CallEdge{
		{
			CallerID: "sym2",
			CalleeID: "sym1",
			FilePath: "/test/other.go",
			Line:     30,
			Col:      10,
		},
	}

	if err := s.WriteIndex(symbols, refs, edges); err != nil {
		t.Fatalf("WriteIndex failed: %v", err)
	}

	// Verify counts
	symCount, refCount, callCount, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if symCount != 2 {
		t.Errorf("Symbol count = %d, want 2", symCount)
	}
	if refCount != 1 {
		t.Errorf("Ref count = %d, want 1", refCount)
	}
	if callCount != 1 {
		t.Errorf("Call count = %d, want 1", callCount)
	}
}

func TestWriteIndexReplace(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// First write
	symbols1 := []index.Symbol{
		{ID: "a", Name: "A", Kind: index.KindFunc, FilePath: "/a.go", LineStart: 1, ColStart: 1, LineEnd: 1, ColEnd: 1},
		{ID: "b", Name: "B", Kind: index.KindFunc, FilePath: "/b.go", LineStart: 1, ColStart: 1, LineEnd: 1, ColEnd: 1},
	}
	if err := s.WriteIndex(symbols1, nil, nil); err != nil {
		t.Fatalf("First WriteIndex failed: %v", err)
	}

	count1, _, _, _ := s.GetStats()
	if count1 != 2 {
		t.Errorf("After first write: count = %d, want 2", count1)
	}

	// Second write should replace
	symbols2 := []index.Symbol{
		{ID: "c", Name: "C", Kind: index.KindFunc, FilePath: "/c.go", LineStart: 1, ColStart: 1, LineEnd: 1, ColEnd: 1},
	}
	if err := s.WriteIndex(symbols2, nil, nil); err != nil {
		t.Fatalf("Second WriteIndex failed: %v", err)
	}

	count2, _, _, _ := s.GetStats()
	if count2 != 1 {
		t.Errorf("After second write: count = %d, want 1", count2)
	}
}

func TestWriteIndexEmpty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Writing empty index should succeed
	if err := s.WriteIndex(nil, nil, nil); err != nil {
		t.Fatalf("WriteIndex with empty data failed: %v", err)
	}

	symCount, refCount, callCount, _ := s.GetStats()
	if symCount != 0 || refCount != 0 || callCount != 0 {
		t.Errorf("Counts should all be 0, got sym=%d ref=%d call=%d", symCount, refCount, callCount)
	}
}
