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

func TestWriteIndexIncremental(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Set repo_root for relative path computation
	_ = s.SetMeta("repo_root", "/repo")

	// Initial full index: 3 files, 3 symbols
	symbols := []index.Symbol{
		{ID: "sym1", Name: "Func1", Kind: index.KindFunc, FilePath: "/repo/a.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym2", Name: "Func2", Kind: index.KindFunc, FilePath: "/repo/b.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym3", Name: "Func3", Kind: index.KindFunc, FilePath: "/repo/c.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
	}
	refs := []index.Ref{
		{ID: "ref1", SymbolID: "sym1", FilePath: "/repo/b.go", Line: 5, Col: 1, Snippet: "Func1()"},
		{ID: "ref2", SymbolID: "sym2", FilePath: "/repo/c.go", Line: 5, Col: 1, Snippet: "Func2()"},
	}
	edges := []index.CallEdge{
		{CallerID: "sym2", CalleeID: "sym1", FilePath: "/repo/b.go", Line: 5, Col: 1},
	}
	if err := s.WriteIndex(symbols, refs, edges); err != nil {
		t.Fatalf("Full WriteIndex failed: %v", err)
	}

	// Verify initial state
	symCount, refCount, callCount, _ := s.GetStats()
	if symCount != 3 || refCount != 2 || callCount != 1 {
		t.Fatalf("Initial state: sym=%d ref=%d call=%d, want 3/2/1", symCount, refCount, callCount)
	}

	// Incremental update: modify b.go (sym2 gets new ID because position changed)
	changedSymbols := []index.Symbol{
		{ID: "sym2v2", Name: "Func2", Kind: index.KindFunc, FilePath: "/repo/b.go", LineStart: 2, ColStart: 1, LineEnd: 12, ColEnd: 1},
	}
	newRefs := []index.Ref{
		{ID: "ref1v2", SymbolID: "sym1", FilePath: "/repo/b.go", Line: 6, Col: 1, Snippet: "Func1()"},
	}
	newEdges := []index.CallEdge{
		{CallerID: "sym2v2", CalleeID: "sym1", FilePath: "/repo/b.go", Line: 6, Col: 1},
	}

	result, err := s.WriteIndexIncremental(changedSymbols, newRefs, newEdges, nil,
		[]string{"/repo/b.go"}, nil)
	if err != nil {
		t.Fatalf("WriteIndexIncremental failed: %v", err)
	}

	// Verify: 3 symbols (sym1, sym2v2, sym3), old sym2 deleted.
	// ref2 in c.go pointed at sym2 — the incremental orphan sweep (#126 F10)
	// removes it, leaving only ref1v2 from b.go.
	symCount, refCount, callCount, _ = s.GetStats()
	_ = callCount
	if symCount != 3 {
		t.Errorf("After incremental: sym=%d, want 3", symCount)
	}
	if refCount != 1 {
		t.Errorf("After incremental: ref=%d, want 1 (orphan swept)", refCount)
	}
	if result.OrphanedRefs != 0 {
		t.Errorf("OrphanedRefs = %d, want 0 after sweep", result.OrphanedRefs)
	}
	if result.IncrementalCount != 1 {
		t.Errorf("IncrementalCount = %d, want 1", result.IncrementalCount)
	}
}

func TestWriteIndexIncremental_DeletedFiles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	_ = s.SetMeta("repo_root", "/repo")

	// Initial: 2 files
	symbols := []index.Symbol{
		{ID: "sym1", Name: "Func1", Kind: index.KindFunc, FilePath: "/repo/a.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym2", Name: "Func2", Kind: index.KindFunc, FilePath: "/repo/b.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
	}
	if err := s.WriteIndex(symbols, nil, nil); err != nil {
		t.Fatalf("Full WriteIndex failed: %v", err)
	}

	symCount, _, _, _ := s.GetStats()
	if symCount != 2 {
		t.Fatalf("Initial sym=%d, want 2", symCount)
	}

	// Delete b.go
	_, err = s.WriteIndexIncremental(nil, nil, nil, nil, nil, []string{"/repo/b.go"})
	if err != nil {
		t.Fatalf("WriteIndexIncremental (delete) failed: %v", err)
	}

	symCount, _, _, _ = s.GetStats()
	if symCount != 1 {
		t.Errorf("After delete: sym=%d, want 1", symCount)
	}
}

func TestWriteIndexIncremental_OrphanedRefs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	_ = s.SetMeta("repo_root", "/repo")

	// Setup: sym1 in a.go, ref to sym1 from b.go
	symbols := []index.Symbol{
		{ID: "sym1", Name: "Func1", Kind: index.KindFunc, FilePath: "/repo/a.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
		{ID: "sym2", Name: "Func2", Kind: index.KindFunc, FilePath: "/repo/b.go", LineStart: 1, ColStart: 1, LineEnd: 10, ColEnd: 1},
	}
	refs := []index.Ref{
		// ref in b.go pointing to sym1
		{ID: "ref1", SymbolID: "sym1", FilePath: "/repo/b.go", Line: 5, Col: 1, Snippet: "Func1()"},
	}
	if err := s.WriteIndex(symbols, refs, nil); err != nil {
		t.Fatalf("Full WriteIndex failed: %v", err)
	}

	// Now modify a.go: sym1 changes position, gets new ID
	changedSymbols := []index.Symbol{
		{ID: "sym1v2", Name: "Func1", Kind: index.KindFunc, FilePath: "/repo/a.go", LineStart: 5, ColStart: 1, LineEnd: 15, ColEnd: 1},
	}

	result, err := s.WriteIndexIncremental(changedSymbols, nil, nil, nil,
		[]string{"/repo/a.go"}, nil)
	if err != nil {
		t.Fatalf("WriteIndexIncremental failed: %v", err)
	}

	// ref1 in b.go pointed to "sym1" which no longer exists. The incremental
	// pass sweeps those rows so the post-reindex count is zero — the audit
	// (snipe #126 F10) surfaced user-visible buildup across many reindexes.
	if result.OrphanedRefs != 0 {
		t.Errorf("OrphanedRefs = %d, want 0 (swept)", result.OrphanedRefs)
	}

	// Verify the orphan is actually gone from the table.
	var leftover int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM refs WHERE symbol_id NOT IN (SELECT id FROM symbols)`).Scan(&leftover); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if leftover != 0 {
		t.Errorf("refs table still has %d orphan rows after incremental sweep", leftover)
	}

	// Verify sym1v2 exists and sym1 is gone
	symCount, _, _, _ := s.GetStats()
	if symCount != 2 {
		t.Errorf("sym count = %d, want 2 (sym1v2 + sym2)", symCount)
	}
}

func TestWriteLiterals(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	refs := []index.StringRef{
		{ID: "abcd1234abcd1234", Value: "MY_KEY", Kind: "env", FilePath: "/f.go", Line: 5, Col: 10},
		{ID: "beef5678beef5678", Value: "MY_CONST", Name: "MyConst", Kind: "const", FilePath: "/f.go", Line: 10, Col: 2},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatalf("WriteLiterals failed: %v", err)
	}

	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM string_refs WHERE value = 'MY_KEY'`).Scan(&count); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 row for MY_KEY, got %d", count)
	}

	// Full replace: write empty set, verify truncated
	if err := s.WriteLiterals(nil, ""); err != nil {
		t.Fatalf("WriteLiterals (empty) failed: %v", err)
	}
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM string_refs`).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0 rows after empty write, got %d", count)
	}
}

func TestWriteLiteralsForFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	refs := []index.StringRef{
		{ID: "aaaa000000000001", Value: "KEY_A", Kind: "env", FilePath: "/a.go", Line: 1, Col: 1},
		{ID: "bbbb000000000001", Value: "KEY_B", Kind: "env", FilePath: "/b.go", Line: 1, Col: 1},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatalf("WriteLiterals failed: %v", err)
	}

	// Incremental: update /a.go with new literal, delete /b.go
	newRefs := []index.StringRef{
		{ID: "aaaa000000000002", Value: "KEY_A2", Kind: "env", FilePath: "/a.go", Line: 2, Col: 1},
	}
	if err := s.WriteLiteralsForFiles(newRefs, []string{"/a.go"}, []string{"/b.go"}, ""); err != nil {
		t.Fatalf("WriteLiteralsForFiles failed: %v", err)
	}

	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM string_refs`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 row (KEY_A2), got %d", count)
	}
	var val string
	if err := s.DB().QueryRow(`SELECT value FROM string_refs`).Scan(&val); err != nil {
		t.Fatalf("value query: %v", err)
	}
	if val != "KEY_A2" {
		t.Errorf("want KEY_A2, got %q", val)
	}
}

// TestWritePackageDocsSweepsOrphans is the regression test for snipe-s5q.
// When a package is renamed/moved, its old pkg_path must not linger in
// package_docs — otherwise consumers (snipe context's project purpose query)
// surface stale doc text from defunct paths.
func TestWritePackageDocsSweepsOrphans(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// First pass: package lives at old path.
	if err := s.WritePackageDocs([]index.PackageDoc{
		{PkgPath: "example.com/proj/cmd/trixi", Doc: "old doc"},
	}); err != nil {
		t.Fatalf("first WritePackageDocs: %v", err)
	}

	// Second pass: package moved to new path; only the new path is reported.
	if err := s.WritePackageDocs([]index.PackageDoc{
		{PkgPath: "example.com/proj/app/cmd/trixi", Doc: "new doc"},
	}); err != nil {
		t.Fatalf("second WritePackageDocs: %v", err)
	}

	rows, err := s.DB().Query(`SELECT pkg_path, doc FROM package_docs ORDER BY pkg_path`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []struct{ path, doc string }
	for rows.Next() {
		var p, d string
		if err := rows.Scan(&p, &d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, struct{ path, doc string }{p, d})
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row after sweep, got %d: %+v", len(got), got)
	}
	if got[0].path != "example.com/proj/app/cmd/trixi" || got[0].doc != "new doc" {
		t.Errorf("orphan not swept; got %+v", got[0])
	}
}

// TestWritePackageDocsEmptyClearsTable verifies that calling with an empty
// set wipes all rows — the load reported zero packages with docs, so any
// surviving rows are by definition orphans.
func TestWritePackageDocsEmptyClearsTable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := s.WritePackageDocs([]index.PackageDoc{
		{PkgPath: "x/y", Doc: "d"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.WritePackageDocs(nil); err != nil {
		t.Fatalf("empty WritePackageDocs: %v", err)
	}
	var n int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM package_docs`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("want empty table, got %d rows", n)
	}
}
