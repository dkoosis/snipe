package graphmetrics

import (
	"reflect"
	"testing"
)

func TestGraphNodes(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
		"d": nil, // isolated node with no out-edges
	}}
	got := g.Nodes()
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Nodes() = %v, want %v", got, want)
	}
}

func TestGraphOutEdges(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
	}}
	if got := g.OutEdges("a"); !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("OutEdges(a) = %v", got)
	}
	if got := g.OutEdges("c"); got != nil {
		t.Errorf("OutEdges(c) = %v, want nil", got)
	}
	if got := g.OutEdges("missing"); got != nil {
		t.Errorf("OutEdges(missing) = %v, want nil", got)
	}
}

func TestGraphInEdges(t *testing.T) {
	g := &Graph{Adj: map[string][]string{
		"a": {"b", "c"},
		"b": {"c"},
		"d": {"c"},
	}}
	got := g.InEdges("c")
	want := []string{"a", "b", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InEdges(c) = %v, want %v", got, want)
	}
	if got := g.InEdges("a"); got != nil {
		t.Errorf("InEdges(a) = %v, want nil", got)
	}
}
