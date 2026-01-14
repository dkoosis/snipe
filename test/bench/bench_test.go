package bench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/search"
	"github.com/dkoosis/snipe/internal/store"
)

// Benchmark targets: snipe's own codebase
var testRepo = filepath.Join("..", "..")

func BenchmarkIndex(b *testing.B) {
	dir, _ := filepath.Abs(testRepo)
	dbPath := filepath.Join(b.TempDir(), "snipe.db")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s, err := store.Open(dbPath)
		if err != nil {
			b.Fatal(err)
		}

		// Load packages
		result, err := index.Load(index.LoadConfig{Dir: dir})
		if err != nil {
			s.Close()
			b.Fatal(err)
		}

		// Extract symbols
		syms, err := index.ExtractSymbols(result)
		if err != nil {
			s.Close()
			b.Fatal(err)
		}

		// Extract refs
		refs, err := index.ExtractRefs(result, syms)
		if err != nil {
			s.Close()
			b.Fatal(err)
		}

		// Extract call graph
		calls, err := index.ExtractCallGraph(result, syms)
		if err != nil {
			s.Close()
			b.Fatal(err)
		}

		// Write to store
		if err := s.WriteIndex(syms, refs, calls); err != nil {
			s.Close()
			b.Fatal(err)
		}
		s.Close()
		os.Remove(dbPath)
	}
}

func BenchmarkDefByName(b *testing.B) {
	dir, _ := filepath.Abs(testRepo)
	s := setupIndex(b, dir)
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = query.LookupByName(s.DB(), "Symbol")
	}
}

func BenchmarkDefByPosition(b *testing.B) {
	dir, _ := filepath.Abs(testRepo)
	s := setupIndex(b, dir)
	defer s.Close()

	pos := &query.PositionQuery{
		File: "internal/store/store.go",
		Line: 20,
		Col:  6,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = query.ResolvePosition(s.DB(), pos)
	}
}

func BenchmarkRefsBySymbolID(b *testing.B) {
	dir, _ := filepath.Abs(testRepo)
	s := setupIndex(b, dir)
	defer s.Close()

	// First find a symbol to get its ID
	syms, _ := query.LookupByName(s.DB(), "Symbol")
	if len(syms) == 0 {
		b.Skip("no Symbol found")
	}
	symbolID := syms[0].ID

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = query.FindRefs(s.DB(), symbolID, 100, 0)
	}
}

func BenchmarkSearch(b *testing.B) {
	dir, _ := filepath.Abs(testRepo)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = search.Search(dir, "func", 50, 0)
	}
}

func BenchmarkSearchRegex(b *testing.B) {
	dir, _ := filepath.Abs(testRepo)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = search.Search(dir, "func.*Error", 50, 0)
	}
}

func setupIndex(b *testing.B, dir string) *store.Store {
	b.Helper()
	dbPath := filepath.Join(b.TempDir(), "snipe.db")

	s, err := store.Open(dbPath)
	if err != nil {
		b.Fatal(err)
	}

	result, err := index.Load(index.LoadConfig{Dir: dir})
	if err != nil {
		s.Close()
		b.Fatal(err)
	}

	syms, err := index.ExtractSymbols(result)
	if err != nil {
		s.Close()
		b.Fatal(err)
	}

	refs, err := index.ExtractRefs(result, syms)
	if err != nil {
		s.Close()
		b.Fatal(err)
	}

	calls, err := index.ExtractCallGraph(result, syms)
	if err != nil {
		s.Close()
		b.Fatal(err)
	}

	if err := s.WriteIndex(syms, refs, calls); err != nil {
		s.Close()
		b.Fatal(err)
	}

	return s
}
