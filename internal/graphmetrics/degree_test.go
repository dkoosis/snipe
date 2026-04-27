package graphmetrics

import (
	"math"
	"testing"
)

func TestInOutDegreeSimple(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
		"c": nil,
		"d": nil, // isolated
	}}
	in := InDegree(g)
	out := OutDegree(g)
	if in["a"] != 0 || in["b"] != 1 || in["c"] != 2 || in["d"] != 0 {
		t.Fatalf("in-degree wrong: %v", in)
	}
	if out["a"] != 2 || out["b"] != 1 || out["c"] != 0 || out["d"] != 0 {
		t.Fatalf("out-degree wrong: %v", out)
	}
}

func TestEigenvectorRingEqual(t *testing.T) {
	// Ring: a->b->c->d->a. All nodes structurally equivalent → equal scores.
	g := &Graph{Adj: map[string][]string{
		"a": {"b"}, "b": {"c"}, "c": {"d"}, "d": {"a"},
	}}
	scores := EigenvectorCentrality(g, 200)
	want := scores["a"]
	for _, v := range []string{"b", "c", "d"} {
		if math.Abs(scores[v]-want) > 1e-6 {
			t.Fatalf("ring not equal: %v", scores)
		}
	}
}

func TestEigenvectorStarCenterHigh(t *testing.T) {
	// Star: leaves all point at center → center should have higher score.
	g := &Graph{Adj: map[string][]string{
		"l1":     {"center"},
		"l2":     {"center"},
		"l3":     {"center"},
		"l4":     {"center"},
		"center": nil,
	}}
	scores := EigenvectorCentrality(g, 200)
	for _, leaf := range []string{"l1", "l2", "l3", "l4"} {
		if scores["center"] <= scores[leaf] {
			t.Fatalf("center not highest: %v", scores)
		}
	}
}
