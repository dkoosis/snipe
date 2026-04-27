package store

import (
	"path/filepath"
	"testing"
)

func TestWriteGraphMetricsAndReadTopN(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = s.Close() }()

	values := map[string]float64{
		"a": 0.10,
		"b": 0.50,
		"c": 0.25,
		"d": 0.05,
		"e": 0.10, // ties with a -> sorted by node_id asc
	}

	if err := s.WriteGraphMetrics("imports", "pagerank", values); err != nil {
		t.Fatalf("WriteGraphMetrics: %v", err)
	}

	got, err := s.ReadTopN("imports", "pagerank", 3)
	if err != nil {
		t.Fatalf("ReadTopN: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ReadTopN n=3 returned %d rows", len(got))
	}
	want := []MetricRow{
		{NodeID: "b", Value: 0.50, Rank: 1},
		{NodeID: "c", Value: 0.25, Rank: 2},
		{NodeID: "a", Value: 0.10, Rank: 3}, // tie-break: a < e
	}
	for i, w := range want {
		if got[i].NodeID != w.NodeID || got[i].Rank != w.Rank || got[i].Value != w.Value {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], w)
		}
	}

	all, err := s.ReadTopN("imports", "pagerank", 0)
	if err != nil {
		t.Fatalf("ReadTopN all: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("ReadTopN all returned %d rows, want 5", len(all))
	}
	// Verify ranks are 1..5 contiguous.
	for i, r := range all {
		if r.Rank != i+1 {
			t.Errorf("row %d rank = %d, want %d", i, r.Rank, i+1)
		}
	}

	// Round-trip again — wipe-and-rewrite semantics.
	if err := s.WriteGraphMetrics("imports", "pagerank", map[string]float64{"x": 1.0}); err != nil {
		t.Fatalf("WriteGraphMetrics rewrite: %v", err)
	}
	all2, err := s.ReadTopN("imports", "pagerank", 0)
	if err != nil {
		t.Fatalf("ReadTopN after rewrite: %v", err)
	}
	if len(all2) != 1 || all2[0].NodeID != "x" || all2[0].Rank != 1 {
		t.Errorf("after rewrite, got %+v, want [{x,1.0,1}]", all2)
	}

	// Different (graph_kind, metric) coexist.
	if err := s.WriteGraphMetrics("calls", "pagerank", map[string]float64{"y": 0.7, "z": 0.3}); err != nil {
		t.Fatalf("WriteGraphMetrics calls: %v", err)
	}
	calls, err := s.ReadTopN("calls", "pagerank", 0)
	if err != nil {
		t.Fatalf("ReadTopN calls: %v", err)
	}
	if len(calls) != 2 || calls[0].NodeID != "y" || calls[1].NodeID != "z" {
		t.Errorf("calls metric: got %+v", calls)
	}
	// imports metric still has x.
	imp, err := s.ReadTopN("imports", "pagerank", 0)
	if err != nil {
		t.Fatalf("ReadTopN imports after second metric: %v", err)
	}
	if len(imp) != 1 || imp[0].NodeID != "x" {
		t.Errorf("imports metric clobbered: got %+v", imp)
	}
}
