//go:build blackbox

package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A repo moved or renamed after indexing keeps an index whose paths name the
// old directory. Queries must refuse it (not answer from it), doctor must
// flag it, and the next snipe index must rebuild it in full (sn-r1do.4).
func TestMovedRepo_IndexIsRefusedThenRebuilt(t *testing.T) {
	oldDir, _ := writeFixture(t)
	indexRepo(t, oldDir)

	newDir := filepath.Join(t.TempDir(), "renamed")
	if err := os.Rename(oldDir, newDir); err != nil {
		t.Fatalf("move repo: %v", err)
	}
	// t.TempDir cleanup expects oldDir to exist.
	t.Cleanup(func() { _ = os.MkdirAll(oldDir, 0o750) })

	stdout, _, exitCode := run(t, newDir, "def", "Callee")
	if exitCode == 0 {
		t.Fatalf("def on a moved repo's index succeeded; want it refused\n%s", stdout)
	}
	resp := parseJSON(t, stdout)
	errObj := requireMap(t, resp["error"], "error")
	if code := getString(t, errObj["code"], "error.code"); code != "INDEX_MISMATCH" {
		t.Fatalf("error.code = %q, want INDEX_MISMATCH", code)
	}
	if msg := getString(t, errObj["message"], "error.message"); !strings.Contains(msg, "renamed") {
		t.Fatalf("error.message %q does not name the new location", msg)
	}

	docOut, _, _ := run(t, newDir, "--format", "concise", "doctor")
	if !strings.Contains(string(docOut), "✗ root-mismatch") {
		t.Fatalf("doctor does not fail root-mismatch on a moved repo:\n%s", docOut)
	}

	_, idxErr, idxExit := run(t, newDir, "index", newDir)
	if idxExit != 0 {
		t.Fatalf("index exit %d: %s", idxExit, idxErr)
	}
	if !strings.Contains(string(idxErr), "running full reindex") {
		t.Fatalf("index on a moved repo did not announce a full reindex:\n%s", idxErr)
	}

	stdout, stderr, exitCode := run(t, newDir, "def", "Callee")
	if exitCode != 0 {
		t.Fatalf("def after reindex exit %d: %s", exitCode, stderr)
	}
	results := requireSlice(t, parseJSON(t, stdout)["results"], "results")
	if len(results) == 0 {
		t.Fatal("def after reindex returned no results")
	}
	file := getString(t, requireMap(t, results[0], "results[0]")["file"], "file")
	if filepath.IsAbs(file) {
		t.Fatalf("def after reindex returned absolute path %q; paths should be repo-relative again", file)
	}
}
