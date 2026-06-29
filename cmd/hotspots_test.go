package cmd

import (
	"math"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

func rowByPath(rows []hotspotRow) map[string]hotspotRow {
	m := make(map[string]hotspotRow, len(rows))
	for _, r := range rows {
		m[r.Path] = r
	}
	return m
}

func TestBuildHotspotsJoinsComplexityAndChurn(t *testing.T) {
	cyclo := []store.MetricRow{
		{NodeID: "hot.go", Value: 100},   // high complexity + high churn → top
		{NodeID: "cold.go", Value: 100},  // same complexity, low churn → lower
		{NodeID: "stable.go", Value: 5},  // low complexity, high churn → lower
		{NodeID: "orphan.go", Value: 80}, // complexity but NO churn → dropped
	}
	maxByPath := map[string]int{"hot.go": 40, "cold.go": 30, "stable.go": 5, "orphan.go": 20}
	cogByPath := map[string]int{"hot.go": 200, "cold.go": 50, "stable.go": 3, "orphan.go": 90}
	fanByPath := map[string]int{"hot.go": 12, "cold.go": 1, "stable.go": 7, "orphan.go": 4}
	churn := map[string]store.FileChurn{
		"hot.go":    {Path: "hot.go", Commits: 50, Authors: 3, LastChanged: "2026-06-01"},
		"cold.go":   {Path: "cold.go", Commits: 2, Authors: 1, LastChanged: "2026-01-01"},
		"stable.go": {Path: "stable.go", Commits: 50, Authors: 2, LastChanged: "2026-06-01"},
	}

	rows := buildHotspots(cyclo, maxByPath, cogByPath, fanByPath, churn, true)
	m := rowByPath(rows)

	if _, ok := m["orphan.go"]; ok {
		t.Errorf("file without churn should be excluded from hotspots, got %+v", rows)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (orphan dropped)", len(rows))
	}
	// hot.go has max complexity AND max churn → highest score.
	if m["hot.go"].Score <= m["cold.go"].Score {
		t.Errorf("hot.go (%.3f) should outscore cold.go (%.3f)", m["hot.go"].Score, m["cold.go"].Score)
	}
	if m["hot.go"].Score <= m["stable.go"].Score {
		t.Errorf("hot.go (%.3f) should outscore stable.go (%.3f)", m["hot.go"].Score, m["stable.go"].Score)
	}
	// Component columns carried through (these ride alongside, not in the score).
	if m["hot.go"].Commits != 50 || m["hot.go"].Authors != 3 || m["hot.go"].CycloMax != 40 {
		t.Errorf("hot.go churn/cyclo fields wrong: %+v", m["hot.go"])
	}
	if m["hot.go"].Cognitive != 200 || m["hot.go"].FanIn != 12 {
		t.Errorf("hot.go cognitive/fan-in not carried: cog=%d in=%d", m["hot.go"].Cognitive, m["hot.go"].FanIn)
	}
}

func TestBuildHotspotsFallsBackToComplexityWithoutChurn(t *testing.T) {
	cyclo := []store.MetricRow{
		{NodeID: "big.go", Value: 200},
		{NodeID: "small.go", Value: 50},
	}
	maxByPath := map[string]int{"big.go": 30, "small.go": 10}
	fanByPath := map[string]int{"big.go": 5, "small.go": 9}

	rows := buildHotspots(cyclo, maxByPath, map[string]int{}, fanByPath, map[string]store.FileChurn{}, false)
	m := rowByPath(rows)

	// No churn → every file scored on complexity alone (no inner-join drop).
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (no churn must not drop files)", len(rows))
	}
	// big.go is the max → normalized score 1.0; small.go is 50/200 = 0.25.
	if m["big.go"].Score != 1.0 {
		t.Errorf("big.go score = %.3f, want 1.0 (complexity-normalized max)", m["big.go"].Score)
	}
	if m["small.go"].Score != 0.25 {
		t.Errorf("small.go score = %.3f, want 0.25", m["small.go"].Score)
	}
	// Fan-in is structural — available and carried even without git churn.
	if m["big.go"].FanIn != 5 || m["small.go"].FanIn != 9 {
		t.Errorf("fan-in should be present without churn: big=%d small=%d", m["big.go"].FanIn, m["small.go"].FanIn)
	}
}

// TestBuildHotspotsScoreReconstruction pins the exact score formula —
// norm(cyclo) × norm(log1p(commits)) — so the displayed score stays
// reconstructable from its component columns. Each case computes the expected
// value the same way the production code does and asserts equality.
func TestBuildHotspotsScoreReconstruction(t *testing.T) {
	const eps = 1e-9
	churn := func(commits int) store.FileChurn {
		return store.FileChurn{Commits: commits, Authors: 1}
	}

	t.Run("equal complexity, churn drives the spread", func(t *testing.T) {
		cyclo := []store.MetricRow{{NodeID: "a", Value: 100}, {NodeID: "b", Value: 100}}
		ch := map[string]store.FileChurn{"a": churn(50), "b": churn(2)}
		m := rowByPath(buildHotspots(cyclo, nil, nil, nil, ch, true))
		maxLog := math.Log1p(50)
		wantA := 1.0 * (math.Log1p(50) / maxLog) // = 1.0
		wantB := 1.0 * (math.Log1p(2) / maxLog)
		if math.Abs(m["a"].Score-wantA) > eps || math.Abs(m["b"].Score-wantB) > eps {
			t.Errorf("scores a=%.6f (want %.6f) b=%.6f (want %.6f)", m["a"].Score, wantA, m["b"].Score, wantB)
		}
	})

	t.Run("equal churn, complexity drives the spread", func(t *testing.T) {
		cyclo := []store.MetricRow{{NodeID: "a", Value: 100}, {NodeID: "b", Value: 25}}
		ch := map[string]store.FileChurn{"a": churn(10), "b": churn(10)}
		m := rowByPath(buildHotspots(cyclo, nil, nil, nil, ch, true))
		// churn factor is 1.0 for both (equal, both == max) → score == cycloNorm.
		if math.Abs(m["a"].Score-1.0) > eps || math.Abs(m["b"].Score-0.25) > eps {
			t.Errorf("scores a=%.6f (want 1.0) b=%.6f (want 0.25)", m["a"].Score, m["b"].Score)
		}
	})

	t.Run("ties produce identical scores", func(t *testing.T) {
		cyclo := []store.MetricRow{{NodeID: "a", Value: 40}, {NodeID: "b", Value: 40}}
		ch := map[string]store.FileChurn{"a": churn(7), "b": churn(7)}
		m := rowByPath(buildHotspots(cyclo, nil, nil, nil, ch, true))
		if m["a"].Score != m["b"].Score {
			t.Errorf("tied inputs gave different scores: a=%v b=%v", m["a"].Score, m["b"].Score)
		}
	})

	t.Run("single row normalizes to 1.0", func(t *testing.T) {
		cyclo := []store.MetricRow{{NodeID: "a", Value: 17}}
		ch := map[string]store.FileChurn{"a": churn(99)}
		m := rowByPath(buildHotspots(cyclo, nil, nil, nil, ch, true))
		if math.Abs(m["a"].Score-1.0) > eps {
			t.Errorf("lone row score = %.6f, want 1.0 (it is both maxes)", m["a"].Score)
		}
	})

	t.Run("zero max complexity → score 0, no divide-by-zero", func(t *testing.T) {
		cyclo := []store.MetricRow{{NodeID: "a", Value: 0}, {NodeID: "b", Value: 0}}
		ch := map[string]store.FileChurn{"a": churn(10), "b": churn(3)}
		rows := buildHotspots(cyclo, nil, nil, nil, ch, true)
		for _, r := range rows {
			if r.Score != 0 || math.IsNaN(r.Score) {
				t.Errorf("%s score = %v, want 0 when maxCyclo==0", r.Path, r.Score)
			}
		}
	})

	t.Run("zero max churn → falls back to complexity, no divide-by-zero", func(t *testing.T) {
		cyclo := []store.MetricRow{{NodeID: "a", Value: 80}, {NodeID: "b", Value: 20}}
		// commits==0 everywhere → log1p(0)==0 → maxLogChurn==0.
		ch := map[string]store.FileChurn{"a": churn(0), "b": churn(0)}
		m := rowByPath(buildHotspots(cyclo, nil, nil, nil, ch, true))
		if math.Abs(m["a"].Score-1.0) > eps || math.Abs(m["b"].Score-0.25) > eps {
			t.Errorf("maxLogChurn==0 should fall back to cycloNorm: a=%.6f b=%.6f", m["a"].Score, m["b"].Score)
		}
	})
}

func TestFilterHotspotsByPath(t *testing.T) {
	rows := []hotspotRow{
		{Path: "internal/store/write.go"},
		{Path: "cmd/index.go"},
		{Path: "internal/store/schema.go"},
	}
	got := filterHotspots(rows, "internal/store", "")
	if len(got) != 2 {
		t.Errorf("pkg filter internal/store → %d rows, want 2: %+v", len(got), got)
	}
}
