//go:build blackbox

package blackbox

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writePlanFixture builds a self-contained module for the `plan` command's
// acceptance cases: a target func (foo.Target) called from two packages (api,
// worker) with a package-level-init call site (NULL enclosing) and a
// func-value use (ast_ctx call:...), a direct + transitive covering test, and
// an orphan func referenced only by a test (delete fast path).
//
// Deliberately NOT an extension of the shared writeFixture: that fixture is
// pinned by ~40 behavioral goldens (context/pkg/deps/diagram are repo-wide),
// none of which are in this bead's edit scope. A private fixture keeps every
// change inside plan_test.go and touches no existing golden.
func writePlanFixture(t *testing.T) (repoDir string, atTarget string) {
	t.Helper()

	repoDir = t.TempDir()
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/planfix\n\ngo 1.20\n")

	fooContent := `package foo

// PLAN_TARGET
func Target(id string) string {
	return "t:" + id
}

func helper() string {
	return Target("h")
}

func Orphan() string {
	return "orphan"
}
`
	writeFile(t, filepath.Join(repoDir, "foo", "foo.go"), fooContent)

	writeFile(t, filepath.Join(repoDir, "foo", "foo_test.go"), `package foo

import "testing"

func TestTarget(t *testing.T) {
	if Target("x") == "" {
		t.Fatal("x")
	}
}

func TestViaHelper(t *testing.T) {
	if helper() == "" {
		t.Fatal("h")
	}
}

func TestOrphan(t *testing.T) {
	if Orphan() == "" {
		t.Fatal("o")
	}
}
`)

	writeFile(t, filepath.Join(repoDir, "api", "api.go"), `package api

import "example.com/planfix/foo"

var _ = foo.Target("init")

func Serve(id string) string {
	return foo.Target(id)
}

func Handle(id string) string {
	v := foo.Target(id)
	return v
}

func Wire() string {
	return callWith(foo.Target)
}

func callWith(f func(string) string) string {
	return f("w")
}

func Dup() {}
`)

	writeFile(t, filepath.Join(repoDir, "worker", "worker.go"), `package worker

import "example.com/planfix/foo"

func Run(id string) string {
	return foo.Target(id)
}

func Dup() {}
`)

	line, col := positionAfterMarker(t, fooContent, "PLAN_TARGET", "Target")
	atTarget = fmt.Sprintf("%s:%d:%d", filepath.Join(repoDir, "foo", "foo.go"), line, col)
	return repoDir, atTarget
}

func planDefID(t *testing.T, repoDir string) string {
	t.Helper()
	jout, jstderr, jexit := run(t, repoDir, "plan", "Target")
	if jexit != 0 {
		t.Fatalf("plan Target --format json exit %d stderr=%s", jexit, string(jstderr))
	}
	resp := assertEnvelope(t, jout, "plan")
	results := requireSlice(t, resp["results"], "results")
	r := requireMap(t, results[0], "results[0]")
	def := requireMap(t, r["def"], "def")
	return getString(t, def["id"], "def.id")
}

func isHex16(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func TestPlan_SignatureWorklist(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	for _, want := range []string{"DEF", "func Target(id string) string", "CALL SITES", "TESTS", "(2-hop)", planNoteFragment} {
		if !strings.Contains(out, want) {
			t.Fatalf("signature output missing %q, got:\n%s", want, out)
		}
	}
	// PRESENT-case ast_ctx: the func-value use `callWith(foo.Target)` yields a
	// populated ast_ctx, which the text renderer must surface as a `[call:...]`
	// tag (complements TestPlan_ASTCtxNullDegrade, which asserts its ABSENCE
	// once ast_ctx is NULLed).
	if !strings.Contains(out, "[call:") {
		t.Fatalf("expected a populated ast_ctx to render a [call:...] tag, got:\n%s", out)
	}

	// JSON: def.id is 16 hex, call_sites non-empty, direct + transitive tests.
	jout, jstderr, jexit := run(t, repoDir, "plan", "Target")
	if jexit != 0 {
		t.Fatalf("plan json exit %d stderr=%s", jexit, string(jstderr))
	}
	resp := assertEnvelope(t, jout, "plan")
	r := requireMap(t, requireSlice(t, resp["results"], "results")[0], "results[0]")
	def := requireMap(t, r["def"], "def")
	if id := getString(t, def["id"], "def.id"); !isHex16(id) {
		t.Fatalf("def.id not 16 hex: %q", id)
	}
	if cs := requireSlice(t, r["call_sites"], "call_sites"); len(cs) == 0 {
		t.Fatalf("expected non-empty call_sites")
	}
	tests := requireMap(t, r["tests"], "tests")
	if len(requireSlice(t, tests["direct"], "tests.direct")) == 0 {
		t.Fatalf("expected direct tests")
	}
	if len(requireSlice(t, tests["transitive"], "tests.transitive")) == 0 {
		t.Fatalf("expected transitive tests")
	}
}

func TestPlan_BehaviorSkipsCallSites(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target", "--change=behavior")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	if strings.Contains(out, "CALL SITES") {
		t.Fatalf("behavior must omit CALL SITES, got:\n%s", out)
	}
	if !strings.Contains(out, "no signature change") {
		t.Fatalf("behavior framing missing, got:\n%s", out)
	}
	if !strings.Contains(out, "TESTS") {
		t.Fatalf("behavior should still list tests, got:\n%s", out)
	}

	jout, _, _ := run(t, repoDir, "plan", "Target", "--change=behavior")
	resp := assertEnvelope(t, jout, "plan")
	r := requireMap(t, requireSlice(t, resp["results"], "results")[0], "results[0]")
	if cs, ok := r["call_sites"]; ok && cs != nil {
		if len(requireSlice(t, cs, "call_sites")) != 0 {
			t.Fatalf("behavior JSON should have empty/absent call_sites, got: %v", cs)
		}
	}
}

func TestPlan_DeleteMustRemove(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target", "--change=delete")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "MUST REMOVE") {
		t.Fatalf("delete must show MUST REMOVE, got:\n%s", out)
	}
	if !strings.Contains(out, "DEF (remove)") {
		t.Fatalf("delete def label missing, got:\n%s", out)
	}

	jout, _, _ := run(t, repoDir, "plan", "Target", "--change=delete")
	resp := assertEnvelope(t, jout, "plan")
	r := requireMap(t, requireSlice(t, resp["results"], "results")[0], "results[0]")
	if !getBool(t, r["must_remove"], "must_remove") {
		t.Fatalf("must_remove should be true")
	}
}

func TestPlan_DeleteZeroCallerFastPath(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Orphan", "--change=delete")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	// Orphan is exported, so the header must NOT claim a bare "safe to delete"
	// — it scopes the claim to the index and flags unseen external consumers.
	if !strings.Contains(out, "no indexed non-test callers") {
		t.Fatalf("expected index-scoped delete claim for exported symbol, got:\n%s", out)
	}
	if !strings.Contains(out, "test-only ref") {
		t.Fatalf("expected test-only refs mention, got:\n%s", out)
	}
	if strings.Contains(out, "MUST REMOVE") {
		t.Fatalf("fast path must not list MUST REMOVE, got:\n%s", out)
	}

	jout, _, _ := run(t, repoDir, "plan", "Orphan", "--change=delete")
	resp := assertEnvelope(t, jout, "plan")
	r := requireMap(t, requireSlice(t, resp["results"], "results")[0], "results[0]")
	if !getBool(t, r["safe_to_delete"], "safe_to_delete") {
		t.Fatalf("safe_to_delete should be true")
	}
	if n := int(getFloat(t, r["test_only_refs"], "test_only_refs")); n != 1 {
		t.Fatalf("want 1 test-only ref, got %d", n)
	}
}

func TestPlan_ASTCtxNullDegrade(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	// Simulate a pre-v18 / not-backfilled index: NULL out every ast_ctx.
	dbPath := filepath.Join(repoDir, ".snipe", "index.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open index db: %v", err)
	}
	if _, err := db.Exec("UPDATE refs SET ast_ctx = NULL"); err != nil {
		db.Close()
		t.Fatalf("null ast_ctx: %v", err)
	}
	db.Close()

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "CALL SITES") {
		t.Fatalf("call sites should still render with NULL ast_ctx, got:\n%s", out)
	}
	if strings.Contains(out, "[call:") {
		t.Fatalf("no bracket tag expected when ast_ctx is NULL, got:\n%s", out)
	}
}

func TestPlan_Truncation(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	// Target has 6 production call sites (foo.helper, api ×4, worker); cap at
	// 2 → footer "+4 more". Derive total from a full run to stay robust.
	jfull, _, _ := run(t, repoDir, "plan", "Target", "--max-callers=100")
	rf := requireMap(t, requireSlice(t, assertEnvelope(t, jfull, "plan")["results"], "results")[0], "results[0]")
	total := int(getFloat(t, rf["total_call_sites"], "total_call_sites"))
	if total <= 2 {
		t.Fatalf("fixture must have >2 call sites for a truncation test, got %d", total)
	}
	wantMore := total - 2

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target", "--max-callers=2")
	if exit != 0 {
		t.Fatalf("plan exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	out := string(stdout)
	wantFooter := fmt.Sprintf("+%d more call sites (raise --max-callers)", wantMore)
	if !strings.Contains(out, wantFooter) {
		t.Fatalf("expected single truncation footer %q, got:\n%s", wantFooter, out)
	}

	jout, _, _ := run(t, repoDir, "plan", "Target", "--max-callers=2")
	resp := assertEnvelope(t, jout, "plan")
	r := requireMap(t, requireSlice(t, resp["results"], "results")[0], "results[0]")
	if n := int(getFloat(t, r["truncated_call_sites"], "truncated_call_sites")); n != wantMore {
		t.Fatalf("want truncated_call_sites=%d, got %d", wantMore, n)
	}
	// Exactly 2 sites emitted across all groups.
	emitted := 0
	for _, g := range requireSlice(t, r["call_sites"], "call_sites") {
		gm := requireMap(t, g, "group")
		emitted += len(requireSlice(t, gm["sites"], "sites"))
	}
	if emitted != 2 {
		t.Fatalf("want 2 emitted sites, got %d", emitted)
	}
}

func TestPlan_PackageLevelCallSite(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	jout, jstderr, jexit := run(t, repoDir, "plan", "Target")
	if jexit != 0 {
		t.Fatalf("plan json exit %d stderr=%s", jexit, string(jstderr))
	}
	resp := assertEnvelope(t, jout, "plan")
	r := requireMap(t, requireSlice(t, resp["results"], "results")[0], "results[0]")

	foundPkgInit := false
	for _, g := range requireSlice(t, r["call_sites"], "call_sites") {
		gm := requireMap(t, g, "group")
		for _, s := range requireSlice(t, gm["sites"], "sites") {
			sm := requireMap(t, s, "site")
			id, _ := sm["id"].(string)
			refID, _ := sm["ref_id"].(string)
			if id == "" && refID != "" {
				foundPkgInit = true
				if !isHex16(refID) {
					t.Fatalf("package-level ref_id not 16 hex: %q", refID)
				}
			}
		}
	}
	if !foundPkgInit {
		t.Fatalf("expected a package-level call site with empty enclosing id but present ref_id, resp:\n%s", string(jout))
	}
}

func TestPlan_ResolveByAtAndID(t *testing.T) {
	repoDir, atTarget := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	// --at resolution (ParsePosition EvalSymlinks the abs path, so the
	// pre-canonical path still resolves under the canonical root).
	stdout, stderr, exit := runRaw(t, repoDir, "plan", "--at", atTarget)
	if exit != 0 {
		t.Fatalf("plan --at exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	if !strings.Contains(string(stdout), "Target") {
		t.Fatalf("plan --at should resolve Target, got:\n%s", string(stdout))
	}

	// --id resolution.
	id := planDefID(t, repoDir)
	stdout, stderr, exit = runRaw(t, repoDir, "plan", "--id", id)
	if exit != 0 {
		t.Fatalf("plan --id exit %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	if !strings.Contains(string(stdout), "DEF") {
		t.Fatalf("plan --id should emit a worklist, got:\n%s", string(stdout))
	}

	// Malformed --at degrades (exit 0, plain message).
	stdout, _, exit = runRaw(t, repoDir, "plan", "--at", "not-a-position")
	if exit != 0 {
		t.Fatalf("malformed --at should exit 0, got %d:\n%s", exit, string(stdout))
	}
	if !strings.Contains(string(stdout), "invalid --at") {
		t.Fatalf("expected invalid --at message, got:\n%s", string(stdout))
	}

	// Unknown --id degrades (exit 0, plain message).
	stdout, _, exit = runRaw(t, repoDir, "plan", "--id", "0000000000000000")
	if exit != 0 {
		t.Fatalf("unknown --id should exit 0, got %d:\n%s", exit, string(stdout))
	}
	if !strings.Contains(string(stdout), "no symbol with id") {
		t.Fatalf("expected no-symbol-with-id message, got:\n%s", string(stdout))
	}
}

func TestPlan_AmbiguousName(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, _, exit := runRaw(t, repoDir, "plan", "Dup")
	if exit != 0 {
		t.Fatalf("ambiguous name should exit 0, got %d:\n%s", exit, string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "ambiguous") {
		t.Fatalf("expected ambiguous message, got:\n%s", out)
	}
}

func TestPlan_MissingIndex(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	// No indexRepo.

	stdout, stderr, exit := runRaw(t, repoDir, "plan", "Target")
	if exit != 0 {
		t.Fatalf("missing index should exit 0, got %d stderr=%s stdout=%s", exit, string(stderr), string(stdout))
	}
	if !strings.Contains(string(stdout), "index unavailable") {
		t.Fatalf("expected index-unavailable message, got:\n%s", string(stdout))
	}
}

func TestPlan_SymbolNotFound(t *testing.T) {
	repoDir, _ := writePlanFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, _, exit := runRaw(t, repoDir, "plan", "NoSuchSymbol")
	if exit != 0 {
		t.Fatalf("unknown symbol should exit 0, got %d:\n%s", exit, string(stdout))
	}
	if !strings.Contains(string(stdout), "no symbol named NoSuchSymbol") {
		t.Fatalf("expected no-symbol-named message, got:\n%s", string(stdout))
	}
}

const planNoteFragment = "not a correctness gate"
