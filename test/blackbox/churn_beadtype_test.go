//go:build blackbox

package blackbox

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// gitCommitTyped stages everything in dir and commits it with msg, which may
// carry Bead-Type trailers (sdlc ADR 0002).
func gitCommitTyped(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// churnRows runs `snipe metrics --kind=churn` with the given extra flags and
// returns the JSON result rows keyed by path.
func churnRows(t *testing.T, repoDir string, extra ...string) ([]any, map[string]map[string]any) {
	t.Helper()
	args := append([]string{"metrics", "--kind=churn"}, extra...)
	out, _, _ := run(t, repoDir, args...)
	rows := requireSlice(t, assertEnvelope(t, out, "metrics")["results"], "results")
	byPath := make(map[string]map[string]any, len(rows))
	for _, r := range rows {
		m := requireMap(t, r, "results[]")
		byPath[getString(t, m["path"], "path")] = m
	}
	return rows, byPath
}

func churnInt(t *testing.T, row map[string]any, key string) int {
	t.Helper()
	f, ok := row[key].(float64)
	if !ok {
		t.Fatalf("row %v: %s missing or not a number", row, key)
	}
	return int(f)
}

// AC 1: a repo whose commits carry `Bead-Type: bug`, `Bead-Type: feature` and
// no trailer reports bug_commits=1, feature_commits=1, untyped_commits=1 on
// the file all three touched.
func TestChurn_BeadTypeCountsPerFile(t *testing.T) {
	repoDir := canonicalRepoDir(t, t.TempDir())
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/churntypes\n\ngo 1.20\n")
	writeFile(t, filepath.Join(repoDir, "hot.go"), "package churntypes\n\nfunc A() {}\n")
	initGitRepo(t, repoDir) // the seed commit carries no trailer → untyped

	writeFile(t, filepath.Join(repoDir, "hot.go"), "package churntypes\n\nfunc A() {}\nfunc B() {}\n")
	gitCommitTyped(t, repoDir, "fix a thing\n\nBead-ID: sn-1\nBead-Type: bug\n")

	writeFile(t, filepath.Join(repoDir, "hot.go"), "package churntypes\n\nfunc A() {}\nfunc B() {}\nfunc C() {}\n")
	gitCommitTyped(t, repoDir, "add a thing\n\nBead-ID: sn-2\nBead-Type: feature\n")

	indexRepo(t, repoDir)

	_, byPath := churnRows(t, repoDir)
	row, ok := byPath["hot.go"]
	if !ok {
		t.Fatalf("no churn row for hot.go; got %v", byPath)
	}
	for _, c := range []struct {
		key  string
		want int
	}{
		{"commits", 3},
		{"bug_commits", 1},
		{"feature_commits", 1},
		{"untyped_commits", 1},
		{"chore_commits", 0},
		{"other_commits", 0},
	} {
		if got := churnInt(t, row, c.key); got != c.want {
			t.Errorf("hot.go %s = %d, want %d", c.key, got, c.want)
		}
	}
}

// AC 2: a repo with no trailers at all indexes cleanly and every row reports
// untyped_commits == commits.
func TestChurn_NoTrailersIsAllUntyped(t *testing.T) {
	repoDir := canonicalRepoDir(t, t.TempDir())
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/churnplain\n\ngo 1.20\n")
	writeFile(t, filepath.Join(repoDir, "a.go"), "package churnplain\n\nfunc A() {}\n")
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	rows, byPath := churnRows(t, repoDir)
	if len(rows) == 0 {
		t.Fatal("no churn rows")
	}
	for path, row := range byPath {
		commits := churnInt(t, row, "commits")
		if got := churnInt(t, row, "untyped_commits"); got != commits {
			t.Errorf("%s: untyped_commits = %d, want commits = %d", path, got, commits)
		}
	}
}

// AC 3: `--by bug` ranks files in descending bug_commits order, which is a
// different order from the default commit-count ranking.
func TestChurn_RankByBug(t *testing.T) {
	repoDir := canonicalRepoDir(t, t.TempDir())
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/churnrank\n\ngo 1.20\n")
	writeFile(t, filepath.Join(repoDir, "busy.go"), "package churnrank\n\nfunc Busy0() {}\n")
	writeFile(t, filepath.Join(repoDir, "buggy.go"), "package churnrank\n\nfunc Buggy0() {}\n")
	initGitRepo(t, repoDir)

	// busy.go churns most but attracts no bug fixes.
	for i := 1; i <= 4; i++ {
		writeFile(t, filepath.Join(repoDir, "busy.go"),
			"package churnrank\n\nfunc Busy0() {}\n"+repeatFuncs("Busy", i))
		gitCommitTyped(t, repoDir, "chore churn\n\nBead-ID: sn-c\nBead-Type: chore\n")
	}
	// buggy.go churns less but every touch is a bug fix.
	for i := 1; i <= 2; i++ {
		writeFile(t, filepath.Join(repoDir, "buggy.go"),
			"package churnrank\n\nfunc Buggy0() {}\n"+repeatFuncs("Buggy", i))
		gitCommitTyped(t, repoDir, "fix\n\nBead-ID: sn-b\nBead-Type: bug\n")
	}

	indexRepo(t, repoDir)

	_, def := churnRows(t, repoDir)
	if churnInt(t, def["busy.go"], "commits") <= churnInt(t, def["buggy.go"], "commits") {
		t.Fatalf("fixture wrong: busy.go should out-churn buggy.go")
	}

	rows, _ := churnRows(t, repoDir, "--by=bug")
	first := requireMap(t, rows[0], "results[0]")
	if got := getString(t, first["path"], "path"); got != "buggy.go" {
		t.Errorf("--by=bug: first row = %q, want buggy.go", got)
	}
	// Descending bug_commits across the whole list.
	prev := -1
	for i, r := range rows {
		n := churnInt(t, requireMap(t, r, "results[]"), "bug_commits")
		if prev >= 0 && n > prev {
			t.Errorf("--by=bug: row %d bug_commits=%d rose above previous %d", i, n, prev)
		}
		prev = n
	}
}

// repeatFuncs returns n distinct no-op funcs so each commit changes the file.
func repeatFuncs(prefix string, n int) string {
	var s string
	for i := 1; i <= n; i++ {
		s += "func " + prefix + string(rune('A'+i-1)) + "() {}\n"
	}
	return s
}
