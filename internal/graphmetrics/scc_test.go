package graphmetrics

import (
	"reflect"
	"testing"
)

func TestSCC_SingleCycle(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"A"},
	}}
	got := SCC(g)
	want := [][]string{{"A", "B", "C"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SCC = %v, want %v", got, want)
	}
}

func TestSCC_TwoDisjointCycles(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B"}, "B": {"A"},
		"X": {"Y"}, "Y": {"Z"}, "Z": {"X"},
	}}
	got := SCC(g)
	// Larger component first; ties broken by first node alphabetical.
	want := [][]string{{"X", "Y", "Z"}, {"A", "B"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SCC = %v, want %v", got, want)
	}
}

func TestSCC_DAG(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B", "C"},
		"B": {"D"},
		"C": {"D"},
		"D": nil,
	}}
	got := SCC(g)
	if len(got) != 0 {
		t.Fatalf("SCC = %v, want empty", got)
	}
}

func TestSCC_SelfLoop(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"A"},
	}}
	got := SCC(g)
	want := [][]string{{"A"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SCC = %v, want %v", got, want)
	}
}

func TestSCC_CycleWithDAGBranch(t *testing.T) {
	// A -> B -> C -> A (cycle) and B -> D -> E (branch), plus F isolated.
	g := &Graph{Adj: map[string][]string{
		"A": {"B"},
		"B": {"C", "D"},
		"C": {"A"},
		"D": {"E"},
		"E": nil,
		"F": nil,
	}}
	got := SCC(g)
	want := [][]string{{"A", "B", "C"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SCC = %v, want %v", got, want)
	}
}

func TestSCC_Empty(t *testing.T) {
	g := &Graph{Adj: map[string][]string{}}
	got := SCC(g)
	if len(got) != 0 {
		t.Fatalf("SCC = %v, want nil/empty", got)
	}
}
