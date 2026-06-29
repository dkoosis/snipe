package store

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

func TestFileFanInCountsDistinctCallerFiles(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	fn := func(id, file string) index.Symbol {
		return index.Symbol{ID: id, Name: id, Kind: index.KindFunc, FilePath: file}
	}
	// a.go is called by two functions in b.go and one in d.go, plus a
	// same-file caller. Distinct caller FILES = 2 (b.go, d.go); distinct
	// caller SYMBOLS would be 3. The "want 2" below pins the file semantics.
	symbols := []index.Symbol{
		fn("a", "a.go"),  // callee — depended on from elsewhere
		fn("b1", "b.go"), // cross-file caller #1 in b.go
		fn("b2", "b.go"), // cross-file caller #2 in b.go (same file as b1)
		fn("d", "d.go"),  // cross-file caller in d.go
		fn("c", "a.go"),  // same-file caller (must be excluded)
	}
	edges := []index.CallEdge{
		{CallerID: "b1", CalleeID: "a"}, // b.go → counts
		{CallerID: "b2", CalleeID: "a"}, // b.go again → same file, no double-count
		{CallerID: "d", CalleeID: "a"},  // d.go → counts
		{CallerID: "c", CalleeID: "a"},  // same file (a.go) → excluded
	}
	if err := s.WriteIndex(symbols, nil, edges); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	fanIn, err := s.FileFanIn()
	if err != nil {
		t.Fatalf("FileFanIn: %v", err)
	}
	if got := fanIn["a.go"]; got != 2 {
		t.Errorf("a.go fan-in = %v, want 2 distinct caller files (b.go, d.go — not 3 symbols, not same-file c)", got)
	}
	if _, ok := fanIn["b.go"]; ok {
		t.Errorf("b.go has no incoming cross-file calls, should be absent: %v", fanIn)
	}
}
