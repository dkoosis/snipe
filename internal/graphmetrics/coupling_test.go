package graphmetrics

import (
	"math"
	"testing"
)

func TestComputeCoupling(t *testing.T) {
	// Graph: a -> b, a -> c, b -> c, d isolated
	g := &Graph{Adj: map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
		"c": nil,
		"d": nil,
	}}
	got := ComputeCoupling(g)

	want := map[string]Coupling{
		"a": {Ca: 0, Ce: 2}, // depends on b, c; nothing depends on a
		"b": {Ca: 1, Ce: 1}, // a depends on b; b depends on c
		"c": {Ca: 2, Ce: 0}, // a, b depend on c; c depends on nothing
		"d": {Ca: 0, Ce: 0},
	}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("node %q: got %+v, want %+v", n, got[n], w)
		}
	}
}

func TestCouplingInstability(t *testing.T) {
	cases := []struct {
		name string
		c    Coupling
		want float64
	}{
		{"isolated", Coupling{0, 0}, 0},
		{"max stable", Coupling{5, 0}, 0},
		{"max unstable", Coupling{0, 5}, 1},
		{"balanced", Coupling{2, 2}, 0.5},
		{"asymmetric", Coupling{1, 3}, 0.75},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.I()
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("I(%+v) = %v, want %v", tc.c, got, tc.want)
			}
		})
	}
}
