package graphmetrics

import "testing"

func TestTopoSortDAG(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b", "c"},
		"b": {"d"},
		"c": {"d"},
		"d": nil,
	}}
	order, cycle := TopoSort(g)
	if cycle != nil {
		t.Fatalf("unexpected cycle: %v", cycle)
	}
	if len(order) != 4 {
		t.Fatalf("order len=%d want 4: %v", len(order), order)
	}
	pos := make(map[string]int)
	for i, n := range order {
		pos[n] = i
	}
	for src, dsts := range g.Adj {
		for _, d := range dsts {
			if pos[src] >= pos[d] {
				t.Fatalf("edge %s->%s violates order: %v", src, d, order)
			}
		}
	}
}

func TestTopoSortCycle(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"}, // cycle
		"d": {"a"}, // feeds cycle but isn't in it
	}}
	order, cycle := TopoSort(g)
	if order != nil {
		t.Fatalf("expected nil order for cycle, got %v", order)
	}
	if len(cycle) == 0 {
		t.Fatalf("expected non-empty cycle witness")
	}
	in := make(map[string]bool)
	for _, n := range cycle {
		in[n] = true
	}
	if !in["a"] && !in["b"] && !in["c"] {
		t.Fatalf("cycle witness missing cycle nodes: %v", cycle)
	}
}
