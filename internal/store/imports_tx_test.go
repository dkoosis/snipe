package store

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

// countImports returns the number of rows in the imports table.
func countImports(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM imports").Scan(&n); err != nil {
		t.Fatalf("count imports: %v", err)
	}
	return n
}

// TestWriteImports_TruncateRepopulateAtomic verifies WriteImports replaces the
// full imports set within a single transaction: stale rows go, new rows land.
func TestWriteImports_TruncateRepopulateAtomic(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// First generation: two imports.
	if err := s.WriteImports([]index.Import{
		{FilePath: "/a.go", PkgPath: "fmt", Line: 1, Col: 1, ImporterPkg: "a"},
		{FilePath: "/a.go", PkgPath: "os", Line: 2, Col: 1, ImporterPkg: "a"},
	}); err != nil {
		t.Fatalf("first WriteImports failed: %v", err)
	}
	if got := countImports(t, s); got != 2 {
		t.Fatalf("after first write: imports = %d, want 2", got)
	}

	// Second generation: one import. The old two must be gone, not accumulated.
	if err := s.WriteImports([]index.Import{
		{FilePath: "/b.go", PkgPath: "io", Line: 1, Col: 1, ImporterPkg: "b"},
	}); err != nil {
		t.Fatalf("second WriteImports failed: %v", err)
	}
	if got := countImports(t, s); got != 1 {
		t.Fatalf("after second write: imports = %d, want 1 (truncate+repopulate not atomic?)", got)
	}
}

// TestWriteIndex_PreservesImportsAcrossPhases is the snipe-syt regression guard.
//
// A full reindex spans multiple transactions: WriteIndex commits symbols first,
// WriteImports commits imports later. The old code truncated `imports` inside
// WriteIndex's tx, so a crash between the two commits left a symbols-populated
// but imports-empty index until the next full reindex.
//
// This test simulates exactly that crash: it runs WriteIndex (the first phase of
// a second reindex) WITHOUT the following WriteImports, then asserts the imports
// table is still populated with the prior generation. Under the buggy code this
// table would be empty. The fix moves the imports truncate into WriteImports' own
// tx, so WriteIndex never exposes an empty imports table.
func TestWriteIndex_PreservesImportsAcrossPhases(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// --- Generation 1: a complete reindex (both phases commit). ---
	gen1Symbols := []index.Symbol{
		{ID: "g1", Name: "Old", Kind: index.KindFunc, FilePath: "/old.go", LineStart: 1, ColStart: 1, LineEnd: 1, ColEnd: 1},
	}
	if err := s.WriteIndex(gen1Symbols, nil, nil); err != nil {
		t.Fatalf("gen1 WriteIndex failed: %v", err)
	}
	if err := s.WriteImports([]index.Import{
		{FilePath: "/old.go", PkgPath: "fmt", Line: 1, Col: 1, ImporterPkg: "old"},
		{FilePath: "/old.go", PkgPath: "os", Line: 2, Col: 1, ImporterPkg: "old"},
	}); err != nil {
		t.Fatalf("gen1 WriteImports failed: %v", err)
	}
	if got := countImports(t, s); got != 2 {
		t.Fatalf("after gen1: imports = %d, want 2", got)
	}

	// --- Generation 2, interrupted: WriteIndex commits new symbols, then the
	// process "crashes" before WriteImports runs. ---
	gen2Symbols := []index.Symbol{
		{ID: "g2", Name: "New", Kind: index.KindFunc, FilePath: "/new.go", LineStart: 1, ColStart: 1, LineEnd: 1, ColEnd: 1},
	}
	if err := s.WriteIndex(gen2Symbols, nil, nil); err != nil {
		t.Fatalf("gen2 WriteIndex failed: %v", err)
	}

	// Symbols reflect the new generation...
	symCount, _, _, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if symCount != 1 {
		t.Fatalf("after gen2 WriteIndex: symbol count = %d, want 1", symCount)
	}

	// ...but imports must NOT be empty. The crash window is closed: WriteIndex no
	// longer truncates imports, so the prior generation's rows survive until the
	// matching WriteImports atomically replaces them.
	if got := countImports(t, s); got == 0 {
		t.Fatalf("imports table empty after WriteIndex without WriteImports: " +
			"a mid-reindex crash exposed a populated-symbols-but-empty-imports index (snipe-syt regression)")
	}
}
