//go:build blackbox

package blackbox

import (
	"fmt"
	"strings"
	"testing"
)

func TestLits_EnvVarCallSites(t *testing.T) {
	// Index the snipe repo itself, then query for a known env var.
	// SNIPE_VOYAGE_API_KEY has 2 os.Getenv calls in internal/embed/client.go.
	indexRepo(t, repoRoot)

	stdout, stderr, exitCode := run(t, repoRoot, "lits", "SNIPE_VOYAGE_API_KEY")
	if exitCode != 0 {
		t.Fatalf("lits exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}

	resp := assertEnvelope(t, stdout, "lits")

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for SNIPE_VOYAGE_API_KEY")
	}

	// All results should be env kind
	for i, r := range results {
		m := requireMap(t, r, fmt.Sprintf("results[%d]", i))
		kind := getString(t, m["kind"], "kind")
		if kind != "env" {
			t.Errorf("results[%d]: want kind=env, got %q", i, kind)
		}
	}

	// All results should reference embed/client.go
	for i, r := range results {
		m := requireMap(t, r, fmt.Sprintf("results[%d]", i))
		file := getString(t, m["file"], "file")
		if !strings.Contains(file, "embed/client.go") {
			t.Errorf("results[%d]: unexpected file %q — expected embed/client.go", i, file)
		}
	}

	// Exactly 2 os.Getenv("SNIPE_VOYAGE_API_KEY") calls exist in the codebase
	meta := requireMap(t, resp["meta"], "meta")
	total := int(getFloat(t, meta["total"], "total"))
	if total != 2 {
		t.Errorf("want 2 refs for SNIPE_VOYAGE_API_KEY, got %d", total)
	}
}
