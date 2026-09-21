package cmd

import (
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

// TestEmbedTextComposition pins what goes into the embedded text of a symbol
// (sn-6wv): name + signature + doc, then the package narrative (first sentence
// of the package doc) and up to embedMaxCallers distinct non-test callers.
func TestEmbedTextComposition(t *testing.T) {
	symbols := []index.Symbol{
		{ID: "t1", Name: "Save", Kind: index.KindFunc, FilePath: "store/save.go", PkgPath: "example.com/m/internal/store",
			Signature: "func Save(x int) error", Doc: "Save persists x."},
		{ID: "c1", Name: "runIndex", Kind: index.KindFunc, FilePath: "cmd/index.go", PkgPath: "example.com/m/cmd", Signature: "func runIndex()"},
		{ID: "c2", Name: "runHeal", Kind: index.KindFunc, FilePath: "cmd/heal.go", PkgPath: "example.com/m/cmd", Signature: "func runHeal()"},
		{ID: "c3", Name: "TestSave", Kind: index.KindFunc, FilePath: "store/save_test.go", PkgPath: "example.com/m/internal/store", Signature: "func TestSave(t *testing.T)"},
		{ID: "n1", Name: "Bare", Kind: index.KindFunc, FilePath: "misc/bare.go", PkgPath: "example.com/m/misc", Signature: "func Bare()"},
		{ID: "v1", Name: "someVar", Kind: index.KindVar, FilePath: "misc/bare.go", PkgPath: "example.com/m/misc", Signature: "var someVar int"},
	}
	edges := []index.CallEdge{
		{CallerID: "c1", CalleeID: "t1"},
		{CallerID: "c1", CalleeID: "t1"}, // duplicate call site: caller listed once
		{CallerID: "c2", CalleeID: "t1"},
		{CallerID: "c3", CalleeID: "t1"}, // test caller: excluded
		{CallerID: "t1", CalleeID: "t1"}, // self-recursion: excluded
	}
	docs := []index.PackageDoc{
		{PkgPath: "example.com/m/internal/store", Doc: "Package store persists the symbol index.\n\nMore detail follows here."},
	}

	got := map[string]string{}
	for _, st := range filterEmbeddableSymbols(symbols, buildEmbedContext(symbols, edges, docs)) {
		got[st.ID] = st.Text
	}

	// Pre-existing composition is a prefix: bare signature + doc unchanged.
	base := "Save func Save(x int) error Save persists x."
	if !strings.HasPrefix(got["t1"], base) {
		t.Errorf("text must start with name+signature+doc %q, got %q", base, got["t1"])
	}
	for _, want := range []string{
		"internal/store", // package location
		"Package store persists the symbol index.", // package narrative, first sentence
		"called by: runHeal, runIndex",             // distinct, sorted, test caller + self excluded
	} {
		if !strings.Contains(got["t1"], want) {
			t.Errorf("text missing %q:\n%s", want, got["t1"])
		}
	}
	for _, unwanted := range []string{"More detail follows", "TestSave", "called by: runHeal, runIndex, Save"} {
		if strings.Contains(got["t1"], unwanted) {
			t.Errorf("text must not contain %q:\n%s", unwanted, got["t1"])
		}
	}

	// A symbol with no callers and no package doc gets no dangling labels.
	if strings.Contains(got["n1"], "called by") {
		t.Errorf("no callers => no 'called by' clause, got %q", got["n1"])
	}
	if !strings.HasPrefix(got["n1"], "Bare func Bare()") {
		t.Errorf("Bare text = %q", got["n1"])
	}
	// Variables stay out of the embedded set.
	if _, ok := got["v1"]; ok {
		t.Errorf("KindVar must not be embedded")
	}
}

// TestEmbedTextCallerCap keeps the token budget bounded: at most
// embedMaxCallers callers are named.
func TestEmbedTextCallerCap(t *testing.T) {
	symbols := []index.Symbol{{ID: "t", Name: "Target", Kind: index.KindFunc, FilePath: "a.go", PkgPath: "m/a", Signature: "func Target()"}}
	var edges []index.CallEdge
	for _, n := range []string{"a1", "a2", "a3", "a4", "a5"} {
		symbols = append(symbols, index.Symbol{ID: n, Name: n, Kind: index.KindFunc, FilePath: "b.go", PkgPath: "m/b", Signature: "func " + n + "()"})
		edges = append(edges, index.CallEdge{CallerID: n, CalleeID: "t"})
	}
	var text string
	for _, st := range filterEmbeddableSymbols(symbols, buildEmbedContext(symbols, edges, nil)) {
		if st.ID == "t" {
			text = st.Text
		}
	}
	if !strings.Contains(text, "called by: a1, a2, a3") || strings.Contains(text, "a4") {
		t.Errorf("expected exactly 3 callers a1..a3, got %q", text)
	}
}
