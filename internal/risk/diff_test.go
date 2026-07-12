package risk

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseDiff_ExtractsHeadSideRanges_When_HunksPresent(t *testing.T) {
	t.Parallel()

	// A two-file diff: one modified file with two hunks, one added file.
	stream := `diff --git a/internal/store/store.go b/internal/store/store.go
index 1111111..2222222 100644
--- a/internal/store/store.go
+++ b/internal/store/store.go
@@ -10,0 +11,3 @@ func Open() {
+	a()
+	b()
+	c()
@@ -40,2 +43 @@ func Close() {
+	d()
diff --git a/cmd/new.go b/cmd/new.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/cmd/new.go
@@ -0,0 +1,2 @@
+package cmd
+
`

	got := parseDiff(stream)
	want := []FileChange{
		{Path: "internal/store/store.go", LineRanges: [][2]int{{11, 13}, {43, 43}}},
		{Path: "cmd/new.go", LineRanges: [][2]int{{1, 2}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDiff mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestParseDiff_SkipsPureDeletions_When_HeadCountZero(t *testing.T) {
	t.Parallel()

	// A file deleted entirely (+++ /dev/null) and a hunk that only removes lines
	// (+0 count) contribute no head-side ranges.
	stream := `diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,5 +0,0 @@
-package gone
diff --git a/trim.go b/trim.go
--- a/trim.go
+++ b/trim.go
@@ -5,3 +4,0 @@ func f() {
-	old()
`

	got := parseDiff(stream)
	// gone.go maps to /dev/null → dropped; trim.go has a hunk with +0 count → no ranges.
	if len(got) != 0 {
		t.Fatalf("expected no head-side changes, got %+v", got)
	}
}

func TestChangedGoFiles_FiltersToGo(t *testing.T) {
	t.Parallel()

	changes := []FileChange{
		{Path: "cmd/risk.go"},
		{Path: "README.md"},
		{Path: "internal/risk/diff.go"},
		{Path: "go.mod"},
	}
	got := changedGoFiles(changes)
	want := []string{"cmd/risk.go", "internal/risk/diff.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedGoFiles=%v want %v", got, want)
	}
}

// --- workingTreeDiff: real git repo fixtures ---
//
// gitDiff/parseDiff are exercised above with hand-built diff streams. The
// working-tree path is easier to trust against a real repo, since staged vs.
// unstaged vs. untracked state is the index/worktree's job to produce, not
// ours to fake.

// gitRepo builds a throwaway repository in a temp dir and returns its root.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Tester")
	return dir
}

// runGit runs a git subcommand in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeFile writes body to name under dir, creating parent dirs as needed.
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAll stages every change in dir and commits it.
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", msg)
}

func TestWorkingTreeDiff_CapturesStagedChanges_When_ChangeIsStagedOnly(t *testing.T) {
	dir := gitRepo(t)
	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	commitAll(t, dir, "init")

	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n\nfunc B() {}\n")
	runGit(t, dir, "add", "a.go")

	changes, ok := workingTreeDiff(dir)
	if !ok {
		t.Fatal("workingTreeDiff not ok")
	}
	want := []FileChange{{Path: "a.go", LineRanges: [][2]int{{4, 5}}}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("got %+v want %+v", changes, want)
	}
}

func TestWorkingTreeDiff_CapturesUnstagedChanges_When_ChangeIsUnstagedOnly(t *testing.T) {
	dir := gitRepo(t)
	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	commitAll(t, dir, "init")

	// Modify without staging.
	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n\nfunc B() {}\n")

	changes, ok := workingTreeDiff(dir)
	if !ok {
		t.Fatal("workingTreeDiff not ok")
	}
	want := []FileChange{{Path: "a.go", LineRanges: [][2]int{{4, 5}}}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("got %+v want %+v", changes, want)
	}
}

func TestWorkingTreeDiff_CapturesMixedChanges_When_OneFileStagedAndAnotherIsNot(t *testing.T) {
	dir := gitRepo(t)
	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n")
	writeFile(t, dir, "b.go", "package p\n\nfunc C() {}\n")
	commitAll(t, dir, "init")

	// a.go: staged change.
	writeFile(t, dir, "a.go", "package p\n\nfunc A() {}\n\nfunc B() {}\n")
	runGit(t, dir, "add", "a.go")
	// b.go: unstaged change.
	writeFile(t, dir, "b.go", "package p\n\nfunc C() {}\n\nfunc D() {}\n")

	changes, ok := workingTreeDiff(dir)
	if !ok {
		t.Fatal("workingTreeDiff not ok")
	}
	want := []FileChange{
		{Path: "a.go", LineRanges: [][2]int{{4, 5}}},
		{Path: "b.go", LineRanges: [][2]int{{4, 5}}},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("got %+v want %+v", changes, want)
	}
}

func TestWorkingTreeDiff_SynthesizesWholeFileChange_When_FileIsUntracked(t *testing.T) {
	dir := gitRepo(t)
	writeFile(t, dir, "a.go", "package p\n")
	commitAll(t, dir, "init")

	writeFile(t, dir, "new.go", "package p\n\nfunc New() {}\n")

	changes, ok := workingTreeDiff(dir)
	if !ok {
		t.Fatal("workingTreeDiff not ok")
	}
	want := []FileChange{{Path: "new.go", LineRanges: [][2]int{{1, 3}}}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("got %+v want %+v", changes, want)
	}
}

func TestWorkingTreeDiff_TracksRenamedFile_When_ContentIsModified(t *testing.T) {
	dir := gitRepo(t)
	writeFile(t, dir, "old.go", "package p\n\nfunc A() {}\n")
	commitAll(t, dir, "init")

	runGit(t, dir, "mv", "old.go", "new.go")
	writeFile(t, dir, "new.go", "package p\n\nfunc A() {}\n\nfunc B() {}\n")
	runGit(t, dir, "add", "new.go")

	changes, ok := workingTreeDiff(dir)
	if !ok {
		t.Fatal("workingTreeDiff not ok")
	}
	want := []FileChange{{Path: "new.go", LineRanges: [][2]int{{4, 5}}}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("got %+v want %+v", changes, want)
	}
}

func TestWorkingTreeDiff_ReportsNoChanges_When_TreeIsClean(t *testing.T) {
	dir := gitRepo(t)
	writeFile(t, dir, "a.go", "package p\n")
	commitAll(t, dir, "init")

	changes, ok := workingTreeDiff(dir)
	if !ok {
		t.Fatal("workingTreeDiff not ok")
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes on a clean tree, got %+v", changes)
	}
}

func TestWorkingTreeDiff_DegradesOk_When_NotAGitRepo(t *testing.T) {
	dir := t.TempDir() // no git init

	changes, ok := workingTreeDiff(dir)
	if ok {
		t.Fatalf("expected ok=false for non-git dir, got changes=%+v", changes)
	}
	if changes != nil {
		t.Errorf("expected nil changes, got %+v", changes)
	}
}
