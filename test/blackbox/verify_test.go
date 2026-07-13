//go:build blackbox

package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// editFile replaces the entire content of an existing fixture file. Used to
// create a working-tree diff after the baseline commit from initGitRepo.
func editFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("edit %s: %v", path, err)
	}
}

// canonicalRepoDir resolves t.TempDir()'s path to its real, symlink-free
// form. On macOS, t.TempDir() lives under /var/folders/... which is itself
// a symlink to /private/var/folders/...; the `index` command stores absolute
// paths built from whatever directory string it's given (no symlink
// resolution), while OpenStore's os.Getwd() (used by verify's own diff-to-
// symbol path join) returns the kernel's canonical, already-resolved cwd.
// Left unreconciled, the two disagree on the repo's absolute path and every
// exact-path symbol match silently returns zero rows. Canonicalizing here
// keeps both sides consistent without touching the shared test harness or
// production path-resolution code.
func canonicalRepoDir(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve symlinks for %s: %v", dir, err)
	}
	return real
}

// TestVerify_CorrectTestSet edits Callee's body (a func with both a direct
// test, TestCallee, and a transitive one via testHelper -> TestViaHelper) and
// asserts verify names both in the paste-ready run line.
//
// Order matters: writeFixture -> initGitRepo (commits baseline) -> EDIT ->
// indexRepo -> verify. Indexing before the edit would leave the index stale,
// and FindOverlappingSymbols' staleness fallback returns every symbol in the
// file instead of just the changed one, breaking the assertions below.
func TestVerify_CorrectTestSet(t *testing.T) {
	repoDir, paths := writeFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)

	editFile(t, paths["main"], strings.Replace(
		readFile(t, paths["main"]),
		"func Callee() string {\n\treturn \"ok\"\n}",
		"func Callee() string {\n\treturn \"OK\"\n}",
		1,
	))

	indexRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "verify")
	if exitCode != 0 {
		t.Fatalf("verify exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
	out := string(stdout)

	if !strings.Contains(out, "tests cover them") {
		t.Fatalf("expected coverage header, got:\n%s", out)
	}
	// -run must be anchored (^(...)$) so it runs exactly the named tests.
	if !strings.Contains(out, "go test . -run '^(") {
		t.Fatalf("expected an anchored root-package go test run line, got:\n%s", out)
	}
	if !strings.Contains(out, "TestCallee") {
		t.Fatalf("expected direct test TestCallee named, got:\n%s", out)
	}
	if !strings.Contains(out, "TestViaHelper") {
		t.Fatalf("expected transitive test TestViaHelper named, got:\n%s", out)
	}
	if strings.Contains(out, "no covering test") {
		t.Fatalf("did not expect an uncovered-symbol warning, got:\n%s", out)
	}

	// JSON envelope: same assertion, structured.
	jout, jstderr, jexit := run(t, repoDir, "verify")
	if jexit != 0 {
		t.Fatalf("verify --format json exit %d stderr=%s", jexit, string(jstderr))
	}
	resp := assertEnvelope(t, jout, "verify")
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := requireMap(t, results[0], "results[0]")
	pkgs := requireSlice(t, r["packages"], "packages")
	if len(pkgs) == 0 {
		t.Fatalf("expected at least 1 package group, got 0")
	}
	foundCallee, foundHelper := false, false
	for _, p := range pkgs {
		pm := requireMap(t, p, "package")
		tests := requireSlice(t, pm["tests"], "tests")
		for _, tst := range tests {
			switch tst.(string) {
			case "TestCallee":
				foundCallee = true
			case "TestViaHelper":
				foundHelper = true
			}
		}
	}
	if !foundCallee || !foundHelper {
		t.Fatalf("expected TestCallee and TestViaHelper in packages, got: %v", pkgs)
	}
}

// TestVerify_EmptyDiff asserts that a fresh, unmodified working tree exits 0
// with a "nothing to verify" message rather than an empty (and misleading)
// go test block.
func TestVerify_EmptyDiff(t *testing.T) {
	repoDir, _ := writeFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "verify")
	if exitCode != 0 {
		t.Fatalf("verify exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "nothing to verify") {
		t.Fatalf("expected a nothing-to-verify message, got:\n%s", out)
	}
}

// TestVerify_NoCoveringTests edits UseAmbiguous, which the fixture never
// tests (nor does its only caller, CallMany), and asserts verify explicitly
// names it as uncovered rather than silently omitting it.
func TestVerify_NoCoveringTests(t *testing.T) {
	repoDir, paths := writeFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)

	editFile(t, paths["main"], strings.Replace(
		readFile(t, paths["main"]),
		"func UseAmbiguous() {\n\talpha.Ambiguous()\n\tbeta.Ambiguous()\n}",
		"func UseAmbiguous() {\n\talpha.Ambiguous()\n\tbeta.Ambiguous()\n\t_ = 1\n}",
		1,
	))

	indexRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "verify")
	if exitCode != 0 {
		t.Fatalf("verify exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
	out := string(stdout)
	if !strings.Contains(out, "no covering test") {
		t.Fatalf("expected a no-covering-test warning, got:\n%s", out)
	}
	if !strings.Contains(out, "UseAmbiguous") {
		t.Fatalf("expected UseAmbiguous named as uncovered, got:\n%s", out)
	}
}

// TestVerify_MissingIndex asserts verify degrades (exit 0 + a clear message)
// on a git repo with no .snipe index, rather than hard-failing. verify is
// never a gate — its doc contract promises degrade-not-fail on a missing
// index, mirroring `snipe risk`. No indexRepo call: the fixture is committed
// but never indexed.
func TestVerify_MissingIndex(t *testing.T) {
	repoDir, _ := writeFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "verify")
	if exitCode != 0 {
		t.Fatalf("verify should exit 0 on missing index, got %d stderr=%s stdout=%s",
			exitCode, string(stderr), string(stdout))
	}
	if !strings.Contains(string(stdout), "index unavailable") {
		t.Fatalf("expected an 'index unavailable' degraded message, got:\n%s", string(stdout))
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
