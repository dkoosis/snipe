package gitchurn

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepo builds a throwaway repository in a temp dir and returns its root.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Tester")
	// Disable any ambient hooks (e.g. a global commit-msg conventional-commit
	// hook via core.hooksPath) so fixture commits aren't rejected.
	run("config", "core.hooksPath", "/dev/null")
	return dir
}

// commit writes files then commits them with a fixed date and author.
func commit(t *testing.T, dir, date, author string, files map[string]string) {
	t.Helper()
	commitMsg(t, dir, date, author, "c", files)
}

// commitMsg is commit with an explicit message, so a test can attach
// Bead-Type trailers (sdlc ADR 0002).
func commitMsg(t *testing.T, dir, date, author, msg string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	add := exec.Command("git", "add", "-A")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	env := []string{
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_DATE=" + date,
		"GIT_AUTHOR_NAME=" + author,
		"GIT_AUTHOR_EMAIL=" + author + "@example.com",
		"GIT_COMMITTER_NAME=" + author,
		"GIT_COMMITTER_EMAIL=" + author + "@example.com",
	}
	c := exec.Command("git", "commit", "-q", "-m", msg)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func byPath(rows []FileChurn) map[string]FileChurn {
	m := make(map[string]FileChurn, len(rows))
	for _, r := range rows {
		m[r.Path] = r
	}
	return m
}

func TestWalkCountsCommitsAndAuthors(t *testing.T) {
	dir := gitRepo(t)
	// hot.go touched by 3 commits across 2 authors; cold.go once.
	commit(t, dir, "2026-01-01T00:00:00Z", "alice", map[string]string{
		"hot.go":  "package p\nvar A int\n",
		"cold.go": "package p\nvar C int\n",
	})
	commit(t, dir, "2026-02-01T00:00:00Z", "bob", map[string]string{
		"hot.go": "package p\nvar A, B int\n",
	})
	commit(t, dir, "2026-03-01T00:00:00Z", "alice", map[string]string{
		"hot.go": "package p\nvar A, B, D int\n",
	})

	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	m := byPath(rows)

	hot, ok := m["hot.go"]
	if !ok {
		t.Fatalf("hot.go missing from %+v", rows)
	}
	if hot.Commits != 3 {
		t.Errorf("hot.go commits = %d, want 3", hot.Commits)
	}
	if hot.Authors != 2 {
		t.Errorf("hot.go authors = %d, want 2 (alice, bob)", hot.Authors)
	}
	if hot.FirstSeen != "2026-01-01" || hot.LastChanged != "2026-03-01" {
		t.Errorf("hot.go span = %s..%s, want 2026-01-01..2026-03-01", hot.FirstSeen, hot.LastChanged)
	}

	cold := m["cold.go"]
	if cold.Commits != 1 {
		t.Errorf("cold.go commits = %d, want 1", cold.Commits)
	}

	// Sorted by commits desc → hot.go first.
	if rows[0].Path != "hot.go" {
		t.Errorf("rows[0] = %s, want hot.go (highest churn)", rows[0].Path)
	}
}

// commitNameEmail commits one file with an explicit, decoupled author name and
// email — used to prove author identity keys on email, not display name.
func commitNameEmail(t *testing.T, dir, date, name, email, file, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "-A")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	env := []string{
		"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date,
		"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email,
		"GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email,
	}
	c := exec.Command("git", "commit", "-q", "-m", "c")
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// TestWalkCountsAuthorsByEmailNotName guards the bus-factor signal against
// user.name drift: one person committing under two display names but a single
// email is one author, not two. (Observed live: "David Koosis" and
// "dkoosis@gmail.com" both at <dkoosis@gmail.com> made every file read auth=2.)
func TestWalkCountsAuthorsByEmailNotName(t *testing.T) {
	dir := gitRepo(t)
	commitNameEmail(t, dir, "2026-01-01T00:00:00Z", "David Koosis", "dk@example.com", "f.go", "package p\nvar A int\n")
	commitNameEmail(t, dir, "2026-02-01T00:00:00Z", "dk@example.com", "dk@example.com", "f.go", "package p\nvar A, B int\n")

	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	f := byPath(rows)["f.go"]
	if f.Commits != 2 {
		t.Errorf("f.go commits = %d, want 2", f.Commits)
	}
	if f.Authors != 1 {
		t.Errorf("f.go authors = %d, want 1 (same email, two display names)", f.Authors)
	}
}

func TestWalkIgnoresNonGoAndMerges(t *testing.T) {
	dir := gitRepo(t)
	commit(t, dir, "2026-01-01T00:00:00Z", "alice", map[string]string{
		"keep.go":   "package p\n",
		"README.md": "# doc\n",
		"go.mod":    "module x\n",
	})
	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	m := byPath(rows)
	if _, ok := m["keep.go"]; !ok {
		t.Errorf("keep.go should be tracked, got %+v", rows)
	}
	if _, ok := m["README.md"]; ok {
		t.Errorf("non-Go README.md should be excluded")
	}
	if _, ok := m["go.mod"]; ok {
		t.Errorf("non-Go go.mod should be excluded")
	}
}

func TestWalkRecencyWeighting(t *testing.T) {
	dir := gitRepo(t)
	// Newer file gets full weight; older file decays below it despite equal commits.
	commit(t, dir, "2026-06-01T00:00:00Z", "alice", map[string]string{"new.go": "package p\n"})
	commit(t, dir, "2020-01-01T00:00:00Z", "alice", map[string]string{"old.go": "package p\n"})
	// Reorder so newest commit (new.go) is the recency reference.
	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	m := byPath(rows)
	if m["new.go"].Score <= m["old.go"].Score {
		t.Errorf("recent file should outscore stale file: new=%.4f old=%.4f",
			m["new.go"].Score, m["old.go"].Score)
	}
}

func TestWalkExcludesDeletedFiles(t *testing.T) {
	dir := gitRepo(t)
	commit(t, dir, "2026-01-01T00:00:00Z", "alice", map[string]string{
		"gone.go": "package p\n",
		"live.go": "package p\n",
	})
	// Delete gone.go in a later commit; its history remains but it is no
	// longer tracked, so it must not appear as a hotspot.
	if err := os.Remove(filepath.Join(dir, "gone.go")); err != nil {
		t.Fatal(err)
	}
	commit(t, dir, "2026-02-01T00:00:00Z", "alice", map[string]string{
		"live.go": "package p\nvar X int\n",
	})

	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	m := byPath(rows)
	if _, ok := m["gone.go"]; ok {
		t.Errorf("deleted gone.go should be excluded, got %+v", rows)
	}
	if _, ok := m["live.go"]; !ok {
		t.Errorf("live.go should remain, got %+v", rows)
	}
}

func TestWalkExcludesVendor(t *testing.T) {
	dir := gitRepo(t)
	commit(t, dir, "2026-01-01T00:00:00Z", "alice", map[string]string{
		"main.go":                 "package p\n",
		"vendor/x.com/dep/dep.go": "package dep\n",
	})
	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	m := byPath(rows)
	if _, ok := m["main.go"]; !ok {
		t.Errorf("main.go should be tracked, got %+v", rows)
	}
	if _, ok := m["vendor/x.com/dep/dep.go"]; ok {
		t.Errorf("vendored file should be excluded, got %+v", rows)
	}
}

func TestWalkNonGitDirIsNoOp(t *testing.T) {
	dir := t.TempDir() // no git init
	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("non-git dir should be a clean no-op, got err: %v", err)
	}
	if rows != nil {
		t.Errorf("non-git dir should yield nil rows, got %+v", rows)
	}
}

func TestWalkEmptyRepoIsNoOp(t *testing.T) {
	dir := gitRepo(t) // init but no commits (unborn branch)
	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("empty repo should be a clean no-op, got err: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty repo should yield no churn, got %+v", rows)
	}
}

func TestWalkClassifiesBeadTypeTrailers(t *testing.T) {
	dir := gitRepo(t)
	// Every commit touches hot.go, so one file carries all four classes.
	commitMsg(t, dir, "2026-01-01T00:00:00Z", "alice",
		"seed\n\nBead-ID: sn-1\nBead-Type: bug\n",
		map[string]string{"hot.go": "package p\nvar A int\n"})
	commitMsg(t, dir, "2026-02-01T00:00:00Z", "alice",
		"add\n\nBead-ID: sn-2\nBead-Type: feature\n",
		map[string]string{"hot.go": "package p\nvar A, B int\n"})
	commitMsg(t, dir, "2026-03-01T00:00:00Z", "alice",
		"tidy\n\nBead-ID: sn-3\nBead-Type: chore\n",
		map[string]string{"hot.go": "package p\nvar A, B, C int\n"})
	commitMsg(t, dir, "2026-04-01T00:00:00Z", "alice",
		"work\n\nBead-ID: sn-4\nBead-Type: task\n",
		map[string]string{"hot.go": "package p\nvar A, B, C, D int\n"})
	commit(t, dir, "2026-05-01T00:00:00Z", "alice",
		map[string]string{"hot.go": "package p\nvar A, B, C, D, E int\n"})

	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := byPath(rows)["hot.go"]
	if got.Commits != 5 {
		t.Fatalf("commits = %d, want 5", got.Commits)
	}
	for _, c := range []struct {
		name string
		got  int
	}{
		{"bug", got.BugCommits},
		{"feature", got.FeatureCommits},
		{"chore", got.ChoreCommits},
		{"other", got.OtherCommits},
		{"untyped", got.UntypedCommits},
	} {
		if c.got != 1 {
			t.Errorf("%s_commits = %d, want 1", c.name, c.got)
		}
	}
	sum := got.BugCommits + got.FeatureCommits + got.ChoreCommits + got.OtherCommits + got.UntypedCommits
	if sum != got.Commits {
		t.Errorf("per-type sum = %d, want commits = %d", sum, got.Commits)
	}
}

func TestWalkUntypedWhenNoTrailers(t *testing.T) {
	dir := gitRepo(t)
	commit(t, dir, "2026-01-01T00:00:00Z", "alice",
		map[string]string{"a.go": "package p\n"})
	commit(t, dir, "2026-02-01T00:00:00Z", "bob",
		map[string]string{"a.go": "package p\nvar X int\n"})

	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no churn rows")
	}
	for _, r := range rows {
		if r.UntypedCommits != r.Commits {
			t.Errorf("%s: untyped = %d, want commits = %d", r.Path, r.UntypedCommits, r.Commits)
		}
		if r.BugCommits+r.FeatureCommits+r.ChoreCommits+r.OtherCommits != 0 {
			t.Errorf("%s: typed buckets non-zero on a trailer-free repo", r.Path)
		}
	}
}

// A commit serving several beads repeats the trailer (ADR 0002 rule 4); it
// counts once, in the highest-precedence bucket.
func TestWalkMultiBeadCommitCountsOnceAsBug(t *testing.T) {
	dir := gitRepo(t)
	commitMsg(t, dir, "2026-01-01T00:00:00Z", "alice",
		"two beads\n\nBead-ID: sn-1\nBead-Type: feature\nBead-ID: sn-2\nBead-Type: bug\n",
		map[string]string{"a.go": "package p\n"})

	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := byPath(rows)["a.go"]
	if got.Commits != 1 || got.BugCommits != 1 || got.FeatureCommits != 0 {
		t.Errorf("commits=%d bug=%d feature=%d, want 1/1/0",
			got.Commits, got.BugCommits, got.FeatureCommits)
	}
}

// ADR 0002 rule 5: a bead-less commit carries `Bead-ID: none` and no type.
func TestWalkBeadlessCommitIsUntyped(t *testing.T) {
	dir := gitRepo(t)
	commitMsg(t, dir, "2026-01-01T00:00:00Z", "alice",
		"no bead\n\nBead-ID: none\n",
		map[string]string{"a.go": "package p\n"})

	rows, err := Walk(dir)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := byPath(rows)["a.go"]
	if got.UntypedCommits != 1 {
		t.Errorf("untyped = %d, want 1", got.UntypedCommits)
	}
}
