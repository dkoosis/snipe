//go:build blackbox

package blackbox

import "testing"

// sn-wa2h — AXI #5 (definitive empty states). A valid-but-empty answer must
// serialize results as [], never null. A nil slice forces every JSON consumer
// to null-guard where an empty array would not. requireSlice fatals on a JSON
// null (it isn't a []any), so it doubles as the regression guard: before the
// fix, `search <no-match>` emitted "results":null and this would fail.
func TestEmptyStateResultsIsArray(t *testing.T) {
	repoDir, _ := writeFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)

	// search runs via ripgrep, no index required — the exact case the bead cites.
	stdout, _, exitCode := run(t, repoDir, "search", "zzz-no-such-symbol-xyz")
	if exitCode != 0 {
		t.Fatalf("no-match search must exit 0 (valid empty answer), got %d", exitCode)
	}

	resp := assertEnvelope(t, stdout, "search")
	results := requireSlice(t, resp["results"], "results") // fatals on null
	if len(results) != 0 {
		t.Fatalf("no-match search: want 0 results, got %d", len(results))
	}
	meta := requireMap(t, resp["meta"], "meta")
	if total, ok := meta["total"].(float64); !ok || total != 0 {
		t.Fatalf("no-match search: meta.total = %v, want 0", meta["total"])
	}
}
