package graphmetrics

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

// TestLoadCallsGraphOpts_ExcludesTestSymbols verifies that when includeTests
// is false, edges where either endpoint lives in a _test.go file are dropped.
// snipe-7fk: prevents test helpers from dominating PageRank.
func TestLoadCallsGraphOpts_ExcludesTestSymbols(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	db := s.DB()
	insertSym := func(id, file string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO symbols (id,name,kind,file_path,line_start,col_start,line_end,col_end) VALUES (?,?,?,?,?,?,?,?)`,
			id, id, "func", file, 1, 1, 1, 1); err != nil {
			t.Fatalf("insert sym %s: %v", id, err)
		}
	}
	insertEdge := func(caller, callee, file string, line int) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO call_graph (caller_id,callee_id,file_path,line,col) VALUES (?,?,?,?,?)`,
			caller, callee, file, line, 1); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}

	// Production symbols.
	insertSym("prodA", "a.go")
	insertSym("prodB", "b.go")
	// Test symbols.
	insertSym("testHelper", "helpers_test.go")
	insertSym("testCase", "x_test.go")

	// prodA -> prodB (production)
	insertEdge("prodA", "prodB", "a.go", 10)
	// testCase -> prodB (test calling production)
	insertEdge("testCase", "prodB", "x_test.go", 20)
	// prodA -> testHelper (production calling test helper — unusual, still filtered)
	insertEdge("prodA", "testHelper", "a.go", 30)

	// includeTests=true: all three edges preserved (3 distinct nodes for src side).
	gAll, err := LoadCallsGraphOpts(s, true)
	if err != nil {
		t.Fatalf("LoadCallsGraphOpts(true): %v", err)
	}
	allNodes := gAll.Nodes()
	sort.Strings(allNodes)
	want := []string{"prodA", "prodB", "testCase", "testHelper"}
	if !equalStrings(allNodes, want) {
		t.Errorf("includeTests=true nodes = %v, want %v", allNodes, want)
	}

	// includeTests=false: only prodA -> prodB survives.
	gProd, err := LoadCallsGraphOpts(s, false)
	if err != nil {
		t.Fatalf("LoadCallsGraphOpts(false): %v", err)
	}
	prodNodes := gProd.Nodes()
	sort.Strings(prodNodes)
	wantProd := []string{"prodA", "prodB"}
	if !equalStrings(prodNodes, wantProd) {
		t.Errorf("includeTests=false nodes = %v, want %v", prodNodes, wantProd)
	}
	if outs := gProd.OutEdges("prodA"); len(outs) != 1 || outs[0] != "prodB" {
		t.Errorf("includeTests=false prodA out = %v, want [prodB]", outs)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
