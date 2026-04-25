//go:build blackbox

package blackbox

import (
	"strings"
	"testing"
)

func TestBoundary_CmdToQuery(t *testing.T) {
	indexRepo(t, repoRoot)

	stdout, stderr, exitCode := run(t, repoRoot,
		"boundary", "cmd", "internal/query", "--format", "json")
	if exitCode != 0 {
		t.Fatalf("boundary exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := assertEnvelope(t, stdout, "boundary")
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}

	r := requireMap(t, results[0], "results[0]")
	dirs := requireSlice(t, r["directions"], "directions")
	if len(dirs) != 2 {
		t.Fatalf("want 2 directions (both), got %d", len(dirs))
	}

	// A→B (cmd → query) must be non-empty: cmd extensively uses query.
	var aToB map[string]any
	for _, d := range dirs {
		m := requireMap(t, d, "direction")
		if getString(t, m["from"], "from") == "A" {
			aToB = m
			break
		}
	}
	if aToB == nil {
		t.Fatal("missing A→B direction")
	}
	if total := int(getFloat(t, aToB["total"], "total")); total < 5 {
		t.Errorf("A→B total: want >=5, got %d", total)
	}

	syms := requireSlice(t, aToB["symbols"], "symbols")
	if len(syms) == 0 {
		t.Fatal("A→B has 0 symbols")
	}

	sawQueryPkg := false
	for _, s := range syms {
		m := requireMap(t, s, "symbol entry")
		tgt := getString(t, m["target_pkg"], "target_pkg")
		if strings.Contains(tgt, "internal/query") {
			sawQueryPkg = true
			break
		}
	}
	if !sawQueryPkg {
		t.Error("no A->B symbol targets internal/query")
	}
}

func TestBoundary_DetailedAddsLocations(t *testing.T) {
	indexRepo(t, repoRoot)

	stdout, _, exitCode := run(t, repoRoot,
		"boundary", "cmd", "internal/query", "--detailed", "--format", "json")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}

	resp := assertEnvelope(t, stdout, "boundary")
	results := requireSlice(t, resp["results"], "results")
	r := requireMap(t, results[0], "results[0]")
	dirs := requireSlice(t, r["directions"], "directions")

	for _, d := range dirs {
		m := requireMap(t, d, "direction")
		if getString(t, m["from"], "from") != "A" {
			continue
		}
		syms := requireSlice(t, m["symbols"], "symbols")
		for _, s := range syms {
			sm := requireMap(t, s, "sym")
			if locs, ok := sm["locations"].([]any); ok && len(locs) > 0 {
				return
			}
		}
	}
	t.Error("--detailed produced zero locations across all A->B symbols")
}
