package graphmetrics

import (
	"math"
	"testing"
)

// 4-node "in-star": A,C,D all point to B. B is no one's intermediary in a
// directed graph (B has no outgoing edges), so all scores are zero.
func TestBetweenness_DirectedInStar(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B"},
		"B": nil,
		"C": {"B"},
		"D": {"B"},
	}}
	bc := Betweenness(g)
	for _, v := range []string{"A", "B", "C", "D"} {
		if math.Abs(bc[v]) > 1e-9 {
			t.Errorf("BC[%s] = %.6f, want 0", v, bc[v])
		}
	}
}

// 4-node "out-star": center hub connects out to leaves AND back. Hub lies on
// every leaf-to-leaf path. With hub H and leaves L1,L2,L3 fully connected
// through H, BC[H] = 1.0 after normalization.
func TestBetweenness_DirectedHub(t *testing.T) {
	// Hub on every shortest path between leaves: leaf -> H -> leaf.
	g := &Graph{Adj: map[string][]string{
		"L1": {"H"},
		"L2": {"H"},
		"L3": {"H"},
		"H":  {"L1", "L2", "L3"},
	}}
	bc := Betweenness(g)
	// 6 ordered leaf pairs (L1,L2), (L1,L3), (L2,L1), (L2,L3), (L3,L1), (L3,L2)
	// each routed through H. Raw BC[H] = 6, normalized by (4-1)(4-2)=6, so 1.0.
	if math.Abs(bc["H"]-1.0) > 1e-9 {
		t.Errorf("BC[H] = %.6f, want 1.0", bc["H"])
	}
	for _, l := range []string{"L1", "L2", "L3"} {
		if math.Abs(bc[l]) > 1e-9 {
			t.Errorf("BC[%s] = %.6f, want 0", l, bc[l])
		}
	}
}

// Path A->B->C->D. B lies on paths (A,C), (A,D); C lies on paths (A,D), (B,D).
// Raw counts: B = 2, C = 2. Normalize by (4-1)(4-2)=6 -> 1/3.
func TestBetweenness_Path(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"D"},
		"D": nil,
	}}
	bc := Betweenness(g)
	want := 2.0 / 6.0
	if math.Abs(bc["B"]-want) > 1e-9 {
		t.Errorf("BC[B] = %.6f, want %.6f", bc["B"], want)
	}
	if math.Abs(bc["C"]-want) > 1e-9 {
		t.Errorf("BC[C] = %.6f, want %.6f", bc["C"], want)
	}
	if math.Abs(bc["A"]) > 1e-9 || math.Abs(bc["D"]) > 1e-9 {
		t.Errorf("endpoints nonzero: A=%.6f D=%.6f", bc["A"], bc["D"])
	}
}

// Disconnected: two components, intermediaries score from within-component
// paths only; nodes with no through-traffic score zero.
func TestBetweenness_Disconnected(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B"}, "B": {"C"}, "C": nil, // path A->B->C
		"X": {"Y"}, "Y": nil, // path X->Y (no intermediaries)
	}}
	bc := Betweenness(g)
	// B intermediates (A,C) only — raw 1, normalized by (5-1)(5-2)=12 -> 1/12.
	want := 1.0 / 12.0
	if math.Abs(bc["B"]-want) > 1e-9 {
		t.Errorf("BC[B] = %.6f, want %.6f", bc["B"], want)
	}
	for _, v := range []string{"A", "C", "X", "Y"} {
		if math.Abs(bc[v]) > 1e-9 {
			t.Errorf("BC[%s] = %.6f, want 0", v, bc[v])
		}
	}
}

func TestBetweenness_EmptyGraph(t *testing.T) {
	g := &Graph{Adj: map[string][]string{}}
	bc := Betweenness(g)
	if len(bc) != 0 {
		t.Errorf("expected empty map, got %v", bc)
	}
}

func TestBetweenness_SingleNode(t *testing.T) {
	g := &Graph{Adj: map[string][]string{"only": nil}}
	bc := Betweenness(g)
	if v, ok := bc["only"]; !ok || v != 0 {
		t.Errorf("BC[only] = %v, want 0", v)
	}
}

func TestBetweenness_TwoNodes(t *testing.T) {
	g := &Graph{Adj: map[string][]string{"A": {"B"}, "B": nil}}
	bc := Betweenness(g)
	for k, v := range bc {
		if v != 0 {
			t.Errorf("BC[%s] = %v, want 0 (n<3)", k, v)
		}
	}
}
