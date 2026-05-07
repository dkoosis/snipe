package index_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

// TestCallGraph_InterfaceDispatch_CrossPkg verifies that an interface call in
// pkg C is wired to a concrete implementation in pkg B, even when C imports
// only A (the interface package) and never imports B directly. Regression for
// the dependency-inversion case that the prior import-set heuristic dropped.
func TestCallGraph_InterfaceDispatch_CrossPkg(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/x\n\ngo 1.21\n")

	// Package A: interface only.
	writeFile(t, filepath.Join(dir, "a", "a.go"), `package a

type Saver interface {
	Save(s string) error
}
`)

	// Package B: concrete implementation. C will NOT import this directly.
	writeFile(t, filepath.Join(dir, "b", "b.go"), `package b

type Store struct{}

func (s *Store) Save(string) error { return nil }
`)

	// Package C: depends on A only; receives the Saver from elsewhere.
	writeFile(t, filepath.Join(dir, "c", "c.go"), `package c

import "example.com/x/a"

func Run(s a.Saver) error { return s.Save("hi") }
`)

	result, err := index.Load(index.LoadConfig{Dir: dir, Patterns: []string{"./..."}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	syms, err := index.ExtractSymbols(result)
	if err != nil {
		t.Fatalf("symbols: %v", err)
	}

	// Find the concrete Save method's symbol ID and the Run caller's ID.
	var saveID, runID string
	for _, s := range syms {
		if s.Kind == index.KindMethod && s.Name == "Save" && strings.HasSuffix(s.FilePath, "/b/b.go") {
			saveID = s.ID
		}
		if s.Kind == index.KindFunc && s.Name == "Run" && strings.HasSuffix(s.FilePath, "/c/c.go") {
			runID = s.ID
		}
	}
	if saveID == "" {
		t.Fatal("expected to find b.Store.Save in symbols")
	}
	if runID == "" {
		t.Fatal("expected to find c.Run in symbols")
	}

	edges, err := index.ExtractCallGraph(result, syms)
	if err != nil {
		t.Fatalf("callgraph: %v", err)
	}

	for _, e := range edges {
		if e.CallerID == runID && e.CalleeID == saveID {
			return // success
		}
	}
	t.Fatalf("missing interface-dispatch edge: c.Run → b.Store.Save (got %d edges)", len(edges))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
