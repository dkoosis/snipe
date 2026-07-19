package risk

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMatchBeads_ExactAndAliasSuffix(t *testing.T) {
	t.Parallel()
	beads := map[string]bead{
		"snipe-aa2": {ID: "snipe-aa2", DependentCount: 5},
		"snipe-vzg": {ID: "snipe-vzg", DependentCount: 9},
		"other-xyz": {ID: "other-xyz", DependentCount: 1},
	}

	// Exact id in a commit message; alias id (sn-vzg) on a branch → suffix match.
	text := "feat/sn-vzg\nfix snipe-aa2: guard nil store\n"
	got := matchBeads(text, beads)

	ids := map[string]bool{}
	for _, b := range got {
		ids[b.ID] = true
	}
	if !ids["snipe-aa2"] || !ids["snipe-vzg"] || len(ids) != 2 {
		t.Fatalf("matched = %v, want exact snipe-aa2 + alias snipe-vzg only", ids)
	}
}

func TestMatchBeads_RejectsNonBeadTokens(t *testing.T) {
	t.Parallel()
	beads := map[string]bead{"snipe-256": {ID: "snipe-256", DependentCount: 4}}
	// sha-256 / utf-8: id-shaped but the final segment bears no letter → no match.
	if got := matchBeads("bump to sha-256 and utf-8 encoding", beads); len(got) != 0 {
		t.Fatalf("expected no match for letterless suffixes, got %+v", got)
	}
}

func TestBeadsSignal_MissingFileIsSilent(t *testing.T) {
	t.Parallel()
	// t.TempDir has no .beads/issues.jsonl → nil, no panic.
	if got := beadsSignal(t.TempDir(), "HEAD~1", "HEAD"); got != nil {
		t.Fatalf("expected nil signal without a beads export, got %+v", got)
	}
}

func TestBeadsSignal_RejectsFlagLikeRefs(t *testing.T) {
	t.Parallel()
	if got := changeProvenance(t.TempDir(), "--output=/tmp/x", "HEAD"); got != "" {
		t.Fatalf("expected empty provenance for a flag-like base ref, got %q", got)
	}
}

// TestBeadsSignal_EndToEnd drives the whole signal through real git: a repo whose
// HEAD commit message names a central bead (dependent_count 7) yields a strong signal.
func TestBeadsSignal_EndToEnd(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			// Isolate from ambient git config/hooks (e.g. a global commit-msg
			// conventional-commit hook via core.hooksPath) so the fixture repo's
			// commits aren't rejected by the developer's environment.
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "base")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("head"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "fix snipe-43i: guard the store")

	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"id":"snipe-43i","dependent_count":7}` + "\n" +
		`{"id":"snipe-xxx","dependent_count":0}` + "\n"
	if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got := beadsSignal(dir, "HEAD~1", "HEAD")
	if len(got) != 1 || got[0].Signal != sigBeadCentral || got[0].Weight != weightStrong {
		t.Fatalf("beadsSignal = %+v, want one strong bead-central", got)
	}
}

func TestLoadBeads_ParsesDependentCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	// Second line is deliberately unparseable — it must be skipped, not fatal.
	body := `{"id":"snipe-aa2","dependent_count":3}` + "\n" +
		`not json` + "\n" +
		`{"id":"snipe-vzg","dependent_count":9}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	beads, ok := loadBeads(path)
	if !ok || len(beads) != 2 {
		t.Fatalf("loadBeads = %v (ok=%v), want 2 valid beads", beads, ok)
	}
	if beads["snipe-vzg"].DependentCount != 9 {
		t.Fatalf("dependent_count = %d, want 9", beads["snipe-vzg"].DependentCount)
	}
}
