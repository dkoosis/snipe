//go:build blackbox

package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

// writeInlineChurnFixture builds a self-contained module whose covering test
// names a fixture path as an INLINE argument (os.ReadFile("testdata/...")) with
// no intervening const. Before sn-8f6q.9 the string_refs extractor indexed only
// os.Getenv args and named consts, so this literal was invisible and `snipe
// plan`'s churn detector missed it. The extractor now indexes fixture-shaped
// inline literals (kind=fixture), so churn must surface it.
//
// Kept separate from writePlanFixture (which uses the const form) so the two
// churn shapes — const and inline — are proven independently and this bead
// touches no existing plan fixture or golden.
func writeInlineChurnFixture(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/inlinechurn\n\ngo 1.20\n")

	writeFile(t, filepath.Join(repoDir, "foo", "foo.go"), `package foo

func Target(id string) string {
	return "t:" + id
}
`)

	// The covering test reads a golden fixture via an inline literal — no const.
	writeFile(t, filepath.Join(repoDir, "foo", "foo_test.go"), `package foo

import (
	"os"
	"testing"
)

func TestTarget(t *testing.T) {
	_, _ = os.ReadFile("testdata/inline.golden")
	if Target("x") == "" {
		t.Fatal("x")
	}
}
`)
	return repoDir
}

// TestPlan_WillChurnInline proves the sn-8f6q.9 broadening end-to-end: a covering
// test with an inline golden-path arg (no const) yields a WILL CHURN entry in
// `snipe plan` — text and JSON.
func TestPlan_WillChurnInline(t *testing.T) {
	repoDir := writeInlineChurnFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "WILL CHURN") {
		t.Fatalf("expected WILL CHURN section for inline fixture arg, got:\n%s", out)
	}
	if !strings.Contains(out, "testdata/inline.golden") {
		t.Fatalf("expected inline churn path testdata/inline.golden, got:\n%s", out)
	}

	// JSON: results[0].churn carries the inline path.
	jout, _, _ := run(t, repoDir, "plan", "Target")
	r := requireMap(t, requireSlice(t, assertEnvelope(t, jout, "plan")["results"], "results")[0], "results[0]")
	churn := requireSlice(t, r["churn"], "churn")
	var found bool
	for _, c := range churn {
		if getString(t, c, "churn[]") == "testdata/inline.golden" {
			found = true
		}
	}
	if !found {
		t.Fatalf("churn JSON missing testdata/inline.golden, got: %v", churn)
	}
}
