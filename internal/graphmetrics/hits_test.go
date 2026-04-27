package graphmetrics

import (
	"math"
	"testing"
)

func sumSquares(m map[string]float64) float64 {
	var s float64
	for _, v := range m {
		s += v * v
	}
	return s
}

func TestHITS_Empty(t *testing.T) {
	g := &Graph{Adj: map[string][]string{}}
	hubs, auths := HITS(g, 50)
	if len(hubs) != 0 || len(auths) != 0 {
		t.Fatalf("expected empty maps, got hubs=%d auths=%d", len(hubs), len(auths))
	}
}

func TestHITS_SingleNode(t *testing.T) {
	g := &Graph{Adj: map[string][]string{"a": nil}}
	hubs, auths := HITS(g, 50)
	if len(hubs) != 1 || len(auths) != 1 {
		t.Fatalf("expected one entry each, got hubs=%d auths=%d", len(hubs), len(auths))
	}
}

// Bipartite: h1, h2 -> a1, a2. Hubs should have hub>0, auth=0.
// Authorities should have auth>0, hub=0.
func TestHITS_Bipartite(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"h1": {"a1", "a2"},
		"h2": {"a1", "a2"},
		"a1": nil,
		"a2": nil,
	}}
	hubs, auths := HITS(g, 100)

	for _, h := range []string{"h1", "h2"} {
		if hubs[h] <= 0.1 {
			t.Errorf("hub %s expected high hub score, got %f", h, hubs[h])
		}
		if auths[h] > 1e-9 {
			t.Errorf("hub %s expected ~0 authority, got %f", h, auths[h])
		}
	}
	for _, a := range []string{"a1", "a2"} {
		if auths[a] <= 0.1 {
			t.Errorf("authority %s expected high auth score, got %f", a, auths[a])
		}
		if hubs[a] > 1e-9 {
			t.Errorf("authority %s expected ~0 hub, got %f", a, hubs[a])
		}
	}

	if math.Abs(sumSquares(hubs)-1.0) > 1e-6 {
		t.Errorf("hubs L2 norm not 1: sum-sq=%f", sumSquares(hubs))
	}
	if math.Abs(sumSquares(auths)-1.0) > 1e-6 {
		t.Errorf("auths L2 norm not 1: sum-sq=%f", sumSquares(auths))
	}
}

// Symmetric directed ring: a->b->c->d->a. By symmetry all scores equal 1/sqrt(N).
func TestHITS_Ring(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b"}, "b": {"c"}, "c": {"d"}, "d": {"a"},
	}}
	hubs, auths := HITS(g, 200)
	want := 1.0 / math.Sqrt(4)
	for _, n := range []string{"a", "b", "c", "d"} {
		if math.Abs(hubs[n]-want) > 1e-3 {
			t.Errorf("hub %s = %f, want %f", n, hubs[n], want)
		}
		if math.Abs(auths[n]-want) > 1e-3 {
			t.Errorf("auth %s = %f, want %f", n, auths[n], want)
		}
	}
}

func TestHITS_L2NormInvariant(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
		"c": {"a"},
		"d": {"a", "b"},
	}}
	hubs, auths := HITS(g, 100)
	if math.Abs(sumSquares(hubs)-1.0) > 1e-6 {
		t.Errorf("hubs sum-sq=%f, want 1.0", sumSquares(hubs))
	}
	if math.Abs(sumSquares(auths)-1.0) > 1e-6 {
		t.Errorf("auths sum-sq=%f, want 1.0", sumSquares(auths))
	}
}
