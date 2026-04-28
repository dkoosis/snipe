package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/query"
)

// snipe-8u1: bare-name resolution must prefer in-repo func/method over
// external/stale candidates, and must error rather than pick a foreign hit
// when nothing in-repo qualifies.
func TestPickFlowEntry(t *testing.T) {
	staleMain := query.SymbolRow{
		ID: "stale1", Name: "main", Kind: "package",
		FilePath: "/Users/x/Library/Caches/go-build/foo.go", FilePathRel: "",
	}
	repoMain := query.SymbolRow{
		ID: "repo1", Name: "main", Kind: "func",
		FilePath: "/repo/cmd/main.go", FilePathRel: "cmd/main.go",
	}
	repoVar := query.SymbolRow{
		ID: "repo2", Name: "main", Kind: "var",
		FilePath: "/repo/cmd/main.go", FilePathRel: "cmd/main.go",
	}

	tests := []struct {
		name    string
		syms    []query.SymbolRow
		wantID  string
		wantErr string
	}{
		{
			name:   "prefers in-repo func over stale package hit",
			syms:   []query.SymbolRow{staleMain, repoMain},
			wantID: "repo1",
		},
		{
			name:   "stale-first ordering still picks in-repo",
			syms:   []query.SymbolRow{staleMain, repoVar, repoMain},
			wantID: "repo1",
		},
		{
			name:   "falls back to in-repo non-func when no func exists",
			syms:   []query.SymbolRow{staleMain, repoVar},
			wantID: "repo2",
		},
		{
			name:    "no in-repo candidate returns clear error",
			syms:    []query.SymbolRow{staleMain},
			wantErr: "outside this repo",
		},
		{
			name:    "empty list returns not-found error",
			syms:    nil,
			wantErr: "not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickFlowEntry("main", tt.syms)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("got ID %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

// snipe-eul: --top-n must rank within the BFS frontier, not intersect with
// global top-N. Acceptance case: BFS produces 8 nodes, top-N=3 returns 3.
func TestTopNWithinFrontier_RanksLocally(t *testing.T) {
	visited := map[string]bool{}
	for _, id := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		visited[id] = true
	}
	// A is the entry — give it a low score on purpose to confirm it is still
	// retained as the anchor.
	scores := map[string]float64{
		"A": 0.01, // entry, low rank
		"B": 0.9,
		"C": 0.8,
		"D": 0.7,
		"E": 0.6,
		"F": 0.5,
		"G": 0.4,
		"H": 0.3,
	}

	got := topNWithinFrontier(visited, scores, 3, "A")
	// Expect: B, C (top 2 by score) + A (forced anchor) = 3 nodes total.
	want := []string{"A", "B", "C"}
	gotKeys := make([]string, 0, len(got))
	for k := range got {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	if len(gotKeys) != len(want) {
		t.Fatalf("got %d nodes %v, want %d %v", len(gotKeys), gotKeys, len(want), want)
	}
	for i := range want {
		if gotKeys[i] != want[i] {
			t.Errorf("got %v, want %v", gotKeys, want)
			break
		}
	}
}

func TestTopNWithinFrontier_EmptyMetricsFallback(t *testing.T) {
	// When the caller passes a nil scores map (simulated upstream
	// "graph_metrics empty" case via the runDiagramFlow guard, but exercising
	// the helper alone with nil scores), every node ranks at 0 and ties break
	// by ID ascending. The N kept must equal min(N, |visited|).
	visited := map[string]bool{"x": true, "y": true, "z": true}
	got := topNWithinFrontier(visited, nil, 2, "x")
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (entry + 1)", len(got))
	}
	if !got["x"] {
		t.Errorf("entry x must be retained as anchor")
	}
}

func TestTopNWithinFrontier_NSmallerThanFrontier(t *testing.T) {
	// Real-world case from snipe-eul: BFS yields >N nodes, helper trims to N.
	visited := map[string]bool{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		visited[id] = true
	}
	scores := map[string]float64{
		"a": 0.5, "b": 0.4, "c": 0.3, "d": 0.2,
		"e": 0.1, "f": 0.05, "g": 0.04, "h": 0.03,
	}
	got := topNWithinFrontier(visited, scores, 3, "a")
	if len(got) != 3 {
		t.Errorf("got %d nodes, want 3 (BFS=8, top-N=3 must NOT collapse to 1)", len(got))
	}
}

func TestTopNWithinFrontier_NoTrimWhenNZero(t *testing.T) {
	visited := map[string]bool{"a": true, "b": true}
	got := topNWithinFrontier(visited, map[string]float64{"a": 1.0}, 0, "a")
	if len(got) != 2 {
		t.Errorf("n=0 must keep all; got %d", len(got))
	}
}
