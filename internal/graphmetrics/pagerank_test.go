package graphmetrics

import (
	"math"
	"testing"
)

func approxEq(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func sumScores(m map[string]float64) float64 {
	var s float64
	for _, v := range m {
		s += v
	}
	return s
}

// Reference 4-node graph: A->B, A->C, B->C, C->A, D->C.
func TestPageRank_ReferenceGraph(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B", "C"},
		"B": {"C"},
		"C": {"A"},
		"D": {"C"},
	}}
	pr := PageRank(g, 0.85, 100)

	want := map[string]float64{"A": 0.372, "B": 0.196, "C": 0.394, "D": 0.038}
	for k, w := range want {
		if !approxEq(pr[k], w, 1e-3) {
			t.Errorf("PR[%s] = %.4f, want %.3f", k, pr[k], w)
		}
	}
	if !approxEq(sumScores(pr), 1.0, 1e-6) {
		t.Errorf("sum = %.9f, want 1.0", sumScores(pr))
	}
}

func TestPageRank_SymmetricRing(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"A"},
	}}
	pr := PageRank(g, 0.85, 100)
	for _, v := range []string{"A", "B", "C"} {
		if !approxEq(pr[v], 1.0/3.0, 1e-4) {
			t.Errorf("PR[%s] = %.6f, want ~0.3333", v, pr[v])
		}
	}
}

func TestPageRank_DanglingNode(t *testing.T) {
	// B has no outedges; mass must not leak.
	g := &Graph{Adj: map[string][]string{
		"A": {"B"},
		"B": nil,
	}}
	pr := PageRank(g, 0.85, 100)
	if !approxEq(sumScores(pr), 1.0, 1e-6) {
		t.Errorf("sum = %.9f, want 1.0 (mass leaked)", sumScores(pr))
	}
}

func TestPageRank_EmptyGraph(t *testing.T) {
	g := &Graph{Adj: map[string][]string{}}
	pr := PageRank(g, 0.85, 50)
	if len(pr) != 0 {
		t.Errorf("expected empty map, got %v", pr)
	}
}

func TestPageRank_SingleNode(t *testing.T) {
	g := &Graph{Adj: map[string][]string{"only": nil}}
	pr := PageRank(g, 0.85, 50)
	if !approxEq(pr["only"], 1.0, 1e-9) {
		t.Errorf("PR[only] = %.6f, want 1.0", pr["only"])
	}
}

func TestPageRank_ConvergesWithin50(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"A": {"B", "C"},
		"B": {"C"},
		"C": {"A"},
		"D": {"C"},
	}}
	pr := PageRank(g, 0.85, 50)
	if !approxEq(sumScores(pr), 1.0, 1e-6) {
		t.Errorf("did not converge cleanly: sum = %.9f", sumScores(pr))
	}
}
