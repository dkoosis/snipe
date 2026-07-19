//go:build blackbox

package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIndex_SymlinkedPathArg_QueryFromCanonicalPath_FindsMatches guards
// sn-za8p: `snipe index <path>` resolved an explicit path arg without
// symlink resolution, while query commands (def --at, verify, risk, ...)
// resolve project root from os.Getwd(), which macOS returns already
// canonicalized (/private/var/... not /var/...). When index was given an
// explicit symlinked path, the two sides disagreed on the file_path values
// they compared against and every query silently returned zero rows. This
// test uses an explicit symlink (rather than relying on macOS's own
// /var -> /private/var symlink) so it exercises the same bug deterministically
// on any platform.
func TestIndex_SymlinkedPathArg_QueryFromCanonicalPath_FindsMatches(t *testing.T) {
	realDir, paths := writeFixture(t)
	if err := os.Mkdir(filepath.Join(realDir, ".git"), 0750); err != nil {
		t.Fatal(err)
	}

	symParent := t.TempDir()
	symDir := filepath.Join(symParent, "symlinked-repo")
	if err := os.Symlink(realDir, symDir); err != nil {
		t.Fatal(err)
	}

	// Index via the symlinked path, from an unrelated cwd — mirrors a CI
	// runner invoking `snipe index /var/folders/.../symlinked-tmp-dir`.
	stdout, stderr, exitCode := run(t, symParent, "index", symDir)
	if exitCode != 0 {
		t.Fatalf("index exit %d stderr=%s stdout=%s", exitCode, stderr, stdout)
	}

	// Query from the canonical (non-symlinked) path via a *relative* --at —
	// cmd/def.go only joins the query-side root (os.Getwd()) onto pos.File
	// when it's relative, so this is the exact site that silently zeroed
	// out under the bug. (paths["callee_at"] is absolute — using it as-is
	// would bypass root resolution entirely and test something else.)
	relAt := strings.TrimPrefix(paths["callee_at"], realDir+string(filepath.Separator))
	stdout, stderr, exitCode = run(t, realDir, "def", "--at", relAt)
	if exitCode != 0 {
		t.Fatalf("def exit %d stderr=%s stdout=%s", exitCode, stderr, stdout)
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected Callee definition querying from canonical path after indexing via symlink, got 0 results (stdout=%s)", stdout)
	}
}
