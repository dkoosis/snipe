package store

import (
	"database/sql"
	"errors"
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

func TestGetAllFiles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Empty initially
	files, err := s.GetAllFiles()
	if err != nil {
		t.Fatalf("GetAllFiles (empty): %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files initially, got %d", len(files))
	}

	// Write some files
	testFiles := []index.FileInfo{
		{Path: "/repo/main.go", Mtime: 1000, Hash: "abc123"},
		{Path: "/repo/util.go", Mtime: 2000, Hash: "def456"},
	}
	if err := s.WriteFiles(testFiles); err != nil {
		t.Fatalf("WriteFiles failed: %v", err)
	}

	// Read them back
	files, err = s.GetAllFiles()
	if err != nil {
		t.Fatalf("GetAllFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	main := files["/repo/main.go"]
	if main.Mtime != 1000 || main.Hash != "abc123" {
		t.Errorf("main.go: mtime=%d hash=%s, want 1000/abc123", main.Mtime, main.Hash)
	}
	util := files["/repo/util.go"]
	if util.Mtime != 2000 || util.Hash != "def456" {
		t.Errorf("util.go: mtime=%d hash=%s, want 2000/def456", util.Mtime, util.Hash)
	}
}

func TestGetAllFilesAfterRewrite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// First write
	if err := s.WriteFiles([]index.FileInfo{
		{Path: "/a.go", Mtime: 100, Hash: "aaa"},
		{Path: "/b.go", Mtime: 200, Hash: "bbb"},
	}); err != nil {
		t.Fatalf("first WriteFiles: %v", err)
	}

	// Second write replaces all
	if err := s.WriteFiles([]index.FileInfo{
		{Path: "/c.go", Mtime: 300, Hash: "ccc"},
	}); err != nil {
		t.Fatalf("second WriteFiles: %v", err)
	}

	files, err := s.GetAllFiles()
	if err != nil {
		t.Fatalf("GetAllFiles: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file after rewrite, got %d", len(files))
	}
	if _, ok := files["/c.go"]; !ok {
		t.Error("expected /c.go in results")
	}
}

func TestWriteIndexPreservesEmbeddings(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// First write: create symbols
	symbols1 := []index.Symbol{
		{ID: "sym1", Name: "Func1", Kind: index.KindFunc, FilePath: "/a.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym2", Name: "Func2", Kind: index.KindFunc, FilePath: "/b.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym3", Name: "Func3", Kind: index.KindFunc, FilePath: "/c.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
	}
	if err := s.WriteIndex(symbols1, nil, nil); err != nil {
		t.Fatalf("First WriteIndex failed: %v", err)
	}

	// Add embeddings for all symbols
	embedding := []float32{0.1, 0.2, 0.3}
	for _, sym := range symbols1 {
		if err := s.SaveEmbedding(sym.ID, embedding, "test-model"); err != nil {
			t.Fatalf("SaveEmbedding failed for %s: %v", sym.ID, err)
		}
	}

	// Verify all embeddings exist
	count1, err := s.CountEmbeddings()
	if err != nil {
		t.Fatalf("CountEmbeddings failed: %v", err)
	}
	if count1 != 3 {
		t.Errorf("Expected 3 embeddings, got %d", count1)
	}

	// Second write: reindex with 2 of the same symbols + 1 new one
	// sym1 and sym2 should keep their embeddings, sym3's embedding should be deleted
	symbols2 := []index.Symbol{
		{ID: "sym1", Name: "Func1", Kind: index.KindFunc, FilePath: "/a.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym2", Name: "Func2", Kind: index.KindFunc, FilePath: "/b.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym4", Name: "Func4", Kind: index.KindFunc, FilePath: "/d.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
	}
	if err := s.WriteIndex(symbols2, nil, nil); err != nil {
		t.Fatalf("Second WriteIndex failed: %v", err)
	}

	// Verify: should have 2 embeddings (sym1, sym2 preserved; sym3 orphaned and deleted)
	count2, err := s.CountEmbeddings()
	if err != nil {
		t.Fatalf("CountEmbeddings after reindex failed: %v", err)
	}
	if count2 != 2 {
		t.Errorf("Expected 2 embeddings after reindex (preserved sym1, sym2), got %d", count2)
	}

	// Verify the correct embeddings remain
	emb1, _, err := s.GetEmbedding("sym1")
	if err != nil || emb1 == nil {
		t.Errorf("sym1 embedding should be preserved, got err=%v emb=%v", err, emb1)
	}
	emb2, _, err := s.GetEmbedding("sym2")
	if err != nil || emb2 == nil {
		t.Errorf("sym2 embedding should be preserved, got err=%v emb=%v", err, emb2)
	}
	emb3, _, err := s.GetEmbedding("sym3")
	if !errors.Is(err, sql.ErrNoRows) || emb3 != nil {
		t.Errorf("sym3 embedding should be deleted (orphaned), got err=%v emb=%v", err, emb3)
	}
	emb4, _, err := s.GetEmbedding("sym4")
	if !errors.Is(err, sql.ErrNoRows) || emb4 != nil {
		t.Errorf("sym4 has no embedding yet (new symbol), got err=%v emb=%v", err, emb4)
	}
}
