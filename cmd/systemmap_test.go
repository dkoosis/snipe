package cmd

import (
	"strings"
	"testing"
)

// sn-l1kh.2: subsystemOf must fold every current internal package into one
// of a small, stable set of comprehension-level "parts of the system" — the
// whole point of the system map is fewer groups than `diagram arch` shows
// packages.
func TestSubsystemOf(t *testing.T) {
	tests := []struct {
		layer string
		want  string
	}{
		{"cmd", subsystemCLI},
		{"root", subsystemRoot},
		{"internal/index", subsystemIndexing},
		{"internal/analyze", subsystemIndexing},
		{"internal/gitchurn", subsystemIndexing},
		{"internal/graphmetrics", subsystemIndexing},
		{"internal/lifecycle", subsystemIndexing},
		{"internal/store", subsystemPersistence},
		{"internal/vector", subsystemPersistence},
		{"internal/kg", subsystemPersistence},
		{"internal/query", subsystemQuery},
		{"internal/search", subsystemQuery},
		{"internal/diagram", subsystemPresentation},
		{"internal/output", subsystemPresentation},
		{"internal/c4", subsystemPresentation},
		{"internal/config", subsystemSupport},
		{"internal/util", subsystemSupport},
		{"internal/telemetry", subsystemSupport},
		// Unknown/future package names must not grow the subsystem count —
		// they fall into Support rather than becoming their own group.
		{"internal/somethingnew", subsystemSupport},
	}
	for _, tt := range tests {
		t.Run(tt.layer, func(t *testing.T) {
			if got := subsystemOf(tt.layer); got != tt.want {
				t.Errorf("subsystemOf(%q) = %q, want %q", tt.layer, got, tt.want)
			}
		})
	}
}

// TestGroupSystemMap_CollapsesIntraGroupEdges is the acceptance case: two
// packages in the same subsystem must produce zero group edges between them,
// while an edge crossing subsystems must survive, deduplicated with a count.
func TestGroupSystemMap_CollapsesIntraGroupEdges(t *testing.T) {
	module := "example.com/mod"
	pkgs := []string{
		module + "/internal/store",
		module + "/internal/vector",  // same subsystem as store (Persistence)
		module + "/internal/query",   // different subsystem (Query)
		module + "/internal/query/x", // layerOf collapses to internal/query too
	}
	edges := [][2]string{
		{module + "/internal/store", module + "/internal/vector"},  // intra-group: dropped
		{module + "/internal/query", module + "/internal/store"},   // cross-group: kept
		{module + "/internal/query/x", module + "/internal/store"}, // cross-group: same pair, count=2
	}

	groups, groupEdges := groupSystemMap(pkgs, edges, module)

	groupNames := map[string]int{}
	for _, g := range groups {
		groupNames[g.name] = len(g.pkgs)
	}
	if groupNames["Persistence"] != 2 {
		t.Errorf("Persistence group = %d pkgs, want 2", groupNames["Persistence"])
	}
	if groupNames["Query"] != 2 {
		t.Errorf("Query group = %d pkgs, want 2 (internal/query and internal/query/x collapse to one layer)", groupNames["Query"])
	}

	if len(groupEdges) != 1 {
		t.Fatalf("got %d group edges, want 1 (intra-Persistence edge dropped, both Query->Persistence edges merged): %+v", len(groupEdges), groupEdges)
	}
	e := groupEdges[0]
	if e.from != "Query" || e.to != "Persistence" {
		t.Errorf("edge = %s -> %s, want Query -> Persistence", e.from, e.to)
	}
	if e.count != 2 {
		t.Errorf("edge count = %d, want 2 (two underlying package edges collapsed)", e.count)
	}
}

func TestGroupSystemMap_NoCrossGroupEdges(t *testing.T) {
	module := "example.com/mod"
	pkgs := []string{module + "/internal/store", module + "/internal/vector"}
	edges := [][2]string{{module + "/internal/store", module + "/internal/vector"}}

	groups, groupEdges := groupSystemMap(pkgs, edges, module)

	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if len(groupEdges) != 0 {
		t.Errorf("got %d group edges, want 0 (single subsystem, all edges intra-group): %+v", len(groupEdges), groupEdges)
	}
}

// TestSystemNodeID_StripsAmpersand is a regression test: diagram.SanitizeID
// doesn't strip "&" (only dots/slashes/spaces/quotes/parens), so a raw
// subsystem name like "Indexing & Analysis" sanitized directly produces
// "Indexing_&_Analysis" — an invalid unquoted D2 identifier that fails to
// compile (`unexpected text after map key`). Caught via a manual smoke test
// of `snipe diagram system-map` against snipe's own index.
func TestSystemNodeID_StripsAmpersand(t *testing.T) {
	got := systemNodeID(subsystemIndexing) // "Indexing & Analysis"
	if strings.Contains(got, "&") {
		t.Errorf("systemNodeID(%q) = %q, contains \"&\" (invalid D2 identifier)", subsystemIndexing, got)
	}
	want := "Indexing_and_Analysis"
	if got != want {
		t.Errorf("systemNodeID(%q) = %q, want %q", subsystemIndexing, got, want)
	}
}

func TestHasHighRank(t *testing.T) {
	ranking := map[string]int{"a": 1, "b": 5, "c": 3}
	if !hasHighRank([]string{"x", "a"}, ranking) {
		t.Error("want true: a ranks 1 (top-3)")
	}
	if hasHighRank([]string{"b"}, ranking) {
		t.Error("want false: b ranks 5 (not top-3)")
	}
	if hasHighRank([]string{"unranked"}, ranking) {
		t.Error("want false: unranked package (rank 0/absent) must never highlight")
	}
	if hasHighRank(nil, ranking) {
		t.Error("want false: empty pkg list")
	}
}
