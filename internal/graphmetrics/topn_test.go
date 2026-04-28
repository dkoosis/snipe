package graphmetrics

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

// TestTopNByPageRank_EmptyMetricsFallback verifies that when graph_metrics has
// no rows for the requested (graphKind, "pagerank") tuple, the helper returns
// (nil, nil, nil) so callers treat it as "keep everything" rather than
// erroring or returning an empty keep-set that would drop every node.
func TestTopNByPageRank_EmptyMetricsFallback(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Nothing written to graph_metrics — the table is empty for both kinds.
	for _, kind := range []string{"imports", "calls"} {
		keep, rank, err := TopNByPageRank(s, kind, 20)
		if err != nil {
			t.Fatalf("TopNByPageRank(%q) error: %v", kind, err)
		}
		if keep != nil {
			t.Errorf("TopNByPageRank(%q): keep = %v, want nil (no-trim fallback)", kind, keep)
		}
		if rank != nil {
			t.Errorf("TopNByPageRank(%q): rank = %v, want nil", kind, rank)
		}
	}
}

// TestTopNByPageRank_NonPositiveTopN: topN <= 0 means "no trim".
func TestTopNByPageRank_NonPositiveTopN(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Even with metrics present, topN<=0 short-circuits to nil.
	if err := s.WriteGraphMetrics("imports", "pagerank", map[string]float64{"a": 1.0}); err != nil {
		t.Fatalf("WriteGraphMetrics: %v", err)
	}
	for _, n := range []int{0, -1} {
		keep, rank, err := TopNByPageRank(s, "imports", n)
		if err != nil {
			t.Fatalf("TopNByPageRank(n=%d): %v", n, err)
		}
		if keep != nil || rank != nil {
			t.Errorf("TopNByPageRank(n=%d): want (nil, nil), got (%v, %v)", n, keep, rank)
		}
	}
}

// TestTopNByPageRank_PopulatedMetrics: returns top-N keep set + rank map.
func TestTopNByPageRank_PopulatedMetrics(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.WriteGraphMetrics("calls", "pagerank", map[string]float64{
		"a": 0.10,
		"b": 0.50,
		"c": 0.25,
		"d": 0.05,
	}); err != nil {
		t.Fatalf("WriteGraphMetrics: %v", err)
	}

	keep, rank, err := TopNByPageRank(s, "calls", 2)
	if err != nil {
		t.Fatalf("TopNByPageRank: %v", err)
	}
	if len(keep) != 2 {
		t.Fatalf("keep size = %d, want 2 (got %v)", len(keep), keep)
	}
	if !keep["b"] || !keep["c"] {
		t.Errorf("expected keep={b,c}, got %v", keep)
	}
	if rank["b"] != 1 || rank["c"] != 2 {
		t.Errorf("rank: got b=%d c=%d, want b=1 c=2", rank["b"], rank["c"])
	}
}
