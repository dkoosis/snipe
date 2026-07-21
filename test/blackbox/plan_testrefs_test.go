//go:build blackbox

package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

// writePlanTestRefFixture builds a module where Target has a production caller
// (foo.Serve) AND a >=3-hop test chain that FindTests' 2-hop reach can't surface:
//
//	TestDeep -> midHelper -> deepHelper -> Target
//
// The `deepHelper -> Target` ref lives in foo_test.go. On the signature and
// delete (MustRemove) paths the old code dropped every _test.go ref and leaned
// on the TESTS list to recover them — but FindTests only reaches Test*-named
// funcs within 2 hops, so this ref surfaced NOWHERE, while the worklist header
// still promised "edit/remove every ref". `deepHelper` is the sentinel: it is
// not a production call site, not a Test* func, and not a churn path, so it
// appears in plan output ONLY via the TEST-FILE REFS section this bead adds.
//
// Self-contained (own module, own helper) so it shares nothing with
// plan_test.go's writePlanFixture — a change here can't perturb that fixture's
// goldens/assertions and vice-versa.
func writePlanTestRefFixture(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/planrefs\n\ngo 1.20\n")

	writeFile(t, filepath.Join(repoDir, "foo", "foo.go"), `package foo

func Target(id string) string {
	return "t:" + id
}

// Serve is a production (non-test) caller, so Target is NOT delete-safe: plan
// takes the MustRemove / signature CALL SITES path, not the fast path.
func Serve(id string) string {
	return Target(id)
}
`)

	writeFile(t, filepath.Join(repoDir, "foo", "foo_test.go"), `package foo

import "testing"

// deepHelper directly refs Target but sits 2 hops below any Test* func — past
// FindTests' 2-hop reach, so the TESTS list never names this ref.
func deepHelper(id string) string {
	return Target(id)
}

func midHelper(id string) string {
	return deepHelper(id)
}

func TestDeep(t *testing.T) {
	if midHelper("x") == "" {
		t.Fatal("x")
	}
}
`)

	return repoDir
}

// assertTestCallSiteEnclosing fails unless results[0].test_call_sites carries a
// site enclosed by the named function.
func assertTestCallSiteEnclosing(t *testing.T, result map[string]any, enclosing string) {
	t.Helper()
	groups, ok := result["test_call_sites"]
	if !ok || groups == nil {
		t.Fatalf("test_call_sites absent; want a site enclosed by %q\nresult: %v", enclosing, result)
	}
	for _, g := range requireSlice(t, groups, "test_call_sites") {
		gm := requireMap(t, g, "group")
		for _, s := range requireSlice(t, gm["sites"], "sites") {
			sm := requireMap(t, s, "site")
			if enc, _ := sm["enclosing"].(string); enc == enclosing {
				return
			}
		}
	}
	t.Fatalf("no test_call_sites entry enclosed by %q\nresult: %v", enclosing, result)
}

// TestPlan_TestFileRefsBeyondFindTests_Signature proves the >=3-hop test ref
// (deepHelper -> Target) is surfaced on the signature path, in text and JSON.
func TestPlan_TestFileRefsBeyondFindTests_Signature(t *testing.T) {
	repoDir := writePlanTestRefFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "TEST-FILE REFS") {
		t.Fatalf("signature must surface a TEST-FILE REFS section, got:\n%s", out)
	}
	if !strings.Contains(out, "deepHelper") {
		t.Fatalf("signature must list the >=3-hop test ref (deepHelper), got:\n%s", out)
	}

	jout, jstderr, jexit := run(t, repoDir, "plan", "Target")
	if jexit != 0 {
		t.Fatalf("plan json exit %d stderr=%s", jexit, string(jstderr))
	}
	r := requireMap(t, requireSlice(t, assertEnvelope(t, jout, "plan")["results"], "results")[0], "results[0]")
	assertTestCallSiteEnclosing(t, r, "deepHelper")
}

// TestPlan_TestFileRefsBeyondFindTests_Delete proves the same ref is surfaced on
// the delete MustRemove path (Target has a production caller, so not the fast
// path).
func TestPlan_TestFileRefsBeyondFindTests_Delete(t *testing.T) {
	repoDir := writePlanTestRefFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target", "--change=delete")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "MUST REMOVE") {
		t.Fatalf("delete with a production caller must be MustRemove, got:\n%s", out)
	}
	if !strings.Contains(out, "TEST-FILE REFS") {
		t.Fatalf("delete must surface a TEST-FILE REFS section, got:\n%s", out)
	}
	if !strings.Contains(out, "deepHelper") {
		t.Fatalf("delete must list the >=3-hop test ref (deepHelper), got:\n%s", out)
	}

	jout, jstderr, jexit := run(t, repoDir, "plan", "Target", "--change=delete")
	if jexit != 0 {
		t.Fatalf("plan delete json exit %d stderr=%s", jexit, string(jstderr))
	}
	r := requireMap(t, requireSlice(t, assertEnvelope(t, jout, "plan")["results"], "results")[0], "results[0]")
	assertTestCallSiteEnclosing(t, r, "deepHelper")
}

// countPlanSites totals the emitted sites across the given result section
// ("call_sites" or "test_call_sites"), summing each package group's sites.
func countPlanSites(t *testing.T, result map[string]any, section string) int {
	t.Helper()
	groups, ok := result[section]
	if !ok || groups == nil {
		return 0
	}
	n := 0
	for _, g := range requireSlice(t, groups, section) {
		gm := requireMap(t, g, "group")
		n += len(requireSlice(t, gm["sites"], "sites"))
	}
	return n
}

// writePlanBudgetFixture builds a module with 3 production call sites AND 2
// test-file call sites of Target — enough that applying --max-callers to each
// partition independently would over-emit.
func writePlanBudgetFixture(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/planbudget\n\ngo 1.20\n")
	writeFile(t, filepath.Join(repoDir, "foo", "foo.go"), `package foo

func Target(id string) string { return "t:" + id }

func Serve1(id string) string { return Target(id) }
func Serve2(id string) string { return Target(id) }
func Serve3(id string) string { return Target(id) }
`)
	writeFile(t, filepath.Join(repoDir, "foo", "foo_test.go"), `package foo

import "testing"

func TestA(t *testing.T) {
	if Target("x") == "" {
		t.Fatal("x")
	}
}

func TestB(t *testing.T) {
	if Target("y") == "" {
		t.Fatal("y")
	}
}
`)
	return repoDir
}

// TestPlan_MaxCallersCapsCombinedSections proves --max-callers bounds the TOTAL
// call sites emitted (commands.go contract), not each of the production and
// test-ref sections independently. With 3 prod + 2 test sites and
// --max-callers=4, the pre-fix code emitted 3+2=5; the shared budget caps the
// combined output at 4.
func TestPlan_MaxCallersCapsCombinedSections(t *testing.T) {
	repoDir := writePlanBudgetFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	const maxCallers = 4
	jout, jstderr, jexit := run(t, repoDir, "plan", "Target", "--max-callers", "4")
	if jexit != 0 {
		t.Fatalf("plan json exit %d stderr=%s", jexit, string(jstderr))
	}
	r := requireMap(t, requireSlice(t, assertEnvelope(t, jout, "plan")["results"], "results")[0], "results[0]")
	prod := countPlanSites(t, r, "call_sites")
	test := countPlanSites(t, r, "test_call_sites")
	if prod+test > maxCallers {
		t.Fatalf("combined emitted sites = %d (prod %d + test %d); --max-callers=%d must cap the total",
			prod+test, prod, test, maxCallers)
	}
	// The production section is primary — it should still claim its share.
	if prod == 0 {
		t.Fatalf("production call_sites empty; expected the primary section to keep its budget\nresult: %v", r)
	}
}

// TestPlan_TestFileRefs_BehaviorOmits guards the scope: a behavior change edits
// no call sites, so it carries no TEST-FILE REFS section.
func TestPlan_TestFileRefs_BehaviorOmits(t *testing.T) {
	repoDir := writePlanTestRefFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target", "--change=behavior")
	if exit != 0 {
		t.Fatalf("behavior exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	if strings.Contains(string(stdout), "TEST-FILE REFS") {
		t.Fatalf("behavior change must omit TEST-FILE REFS, got:\n%s", string(stdout))
	}
}
