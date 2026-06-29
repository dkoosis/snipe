package store

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

func TestFileFanInCountsCrossFileCallersOnly(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	fn := func(id, file string) index.Symbol {
		return index.Symbol{ID: id, Name: id, Kind: index.KindFunc, FilePath: file}
	}
	symbols := []index.Symbol{
		fn("a", "a.go"), // callee — depended on from elsewhere
		fn("b", "b.go"), // cross-file caller
		fn("d", "d.go"), // cross-file caller
		fn("c", "a.go"), // same-file caller (must be excluded)
	}
	edges := []index.CallEdge{
		{CallerID: "b", CalleeID: "a"}, // cross-file → counts
		{CallerID: "d", CalleeID: "a"}, // cross-file → counts
		{CallerID: "c", CalleeID: "a"}, // same file (a.go) → excluded
	}
	if err := s.WriteIndex(symbols, nil, edges); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	fanIn, err := s.FileFanIn()
	if err != nil {
		t.Fatalf("FileFanIn: %v", err)
	}
	if got := fanIn["a.go"]; got != 2 {
		t.Errorf("a.go fan-in = %v, want 2 (b, d — not same-file c)", got)
	}
	if _, ok := fanIn["b.go"]; ok {
		t.Errorf("b.go has no incoming cross-file calls, should be absent: %v", fanIn)
	}
}
