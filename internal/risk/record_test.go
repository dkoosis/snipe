package risk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A ref that moves after risk resolved it must not move the record: Append
// keeps the commits it was handed, not what the refs name when it runs.
func TestAppend_KeepsTheCommitsResolvedBeforeMeasuring(t *testing.T) {
	t.Setenv("SNIPE_NO_TELEMETRY", "")
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "config", "core.hooksPath", "/dev/null")
	commit := func(msg string) {
		gitRun(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", msg)
	}
	commit("one")
	commit("two")
	if err := os.Mkdir(filepath.Join(repo, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}

	base, head := Commit(repo, "HEAD~1"), Commit(repo, "HEAD")
	if base == "" || head == "" || base == head {
		t.Fatalf("Commit: base=%q head=%q, want two distinct SHAs", base, head)
	}
	commit("three") // HEAD moves while risk is measuring

	Append(repo, Record{Base: "HEAD~1", Head: "HEAD", BaseSHA: base, HeadSHA: head})

	data, err := os.ReadFile(filepath.Join(repo, ".snipe", RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	var rec Record
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.BaseSHA != base || rec.HeadSHA != head {
		t.Fatalf("record SHAs = %s..%s, want the pinned %s..%s (HEAD is now %s)",
			rec.BaseSHA, rec.HeadSHA, base, head, Commit(repo, "HEAD"))
	}
	if rec.TS == "" {
		t.Fatal("record has no timestamp")
	}
}

func TestCommit_EmptyWhenTheRefDoesNotResolve(t *testing.T) {
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	for _, ref := range []string{"", "no-such-ref", "--output=/tmp/x"} {
		if got := Commit(repo, ref); got != "" {
			t.Errorf("Commit(%q) = %q, want empty", ref, got)
		}
	}
	if got := Commit("", "HEAD"); got != "" {
		t.Errorf("Commit with no root = %q, want empty", got)
	}
}
