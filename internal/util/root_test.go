// internal/util/root_test.go
package util_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/util"
)

func TestFindProjectRoot_FindsGitRoot_WhenRunFromSubdirectory(t *testing.T) {
	t.Parallel()

	// Layout: tmpdir/.git + tmpdir/sub/sub2/
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0750); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(tmp, "sub", "sub2")
	if err := os.MkdirAll(sub, 0750); err != nil {
		t.Fatal(err)
	}

	got := util.FindProjectRoot(sub)
	if want := canonical(t, tmp); got != want {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", sub, got, want)
	}
}

func TestFindProjectRoot_FindsGoModRoot_WhenNoGit(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/foo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(tmp, "pkg")
	if err := os.Mkdir(sub, 0750); err != nil {
		t.Fatal(err)
	}

	got := util.FindProjectRoot(sub)
	if want := canonical(t, tmp); got != want {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", sub, got, want)
	}
}

func TestFindProjectRoot_PrefersGitOverGoMod_WhenBothPresent(t *testing.T) {
	t.Parallel()

	// gomod at tmp/, .git at tmp/sub/
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/foo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(tmp, "sub")
	if err := os.Mkdir(sub, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sub, ".git"), 0750); err != nil {
		t.Fatal(err)
	}

	got := util.FindProjectRoot(sub)
	if want := canonical(t, sub); got != want {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", sub, got, want)
	}
}

func TestFindProjectRoot_PrefersGitRoot_WhenGitIsHigherThanGoMod(t *testing.T) {
	t.Parallel()

	// Monorepo case: .git at tmp/, go.mod at tmp/service/
	// Running from tmp/service/pkg/ should return tmp/ (the .git root), not tmp/service/
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0750); err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(tmp, "service")
	if err := os.Mkdir(service, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "go.mod"), []byte("module example.com/service\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(service, "pkg")
	if err := os.Mkdir(pkg, 0750); err != nil {
		t.Fatal(err)
	}

	got := util.FindProjectRoot(pkg)
	if want := canonical(t, tmp); got != want {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q (git root)", pkg, got, want)
	}
}

func TestFindProjectRoot_GitWins_WhenGitIsDeeperThanGoMod(t *testing.T) {
	t.Parallel()

	// Inverse monorepo: go.mod at tmp/, .git at tmp/service/
	// The two-pass algorithm finds .git first (at tmp/service/) and returns it,
	// never falling through to go.mod. Documents that .git always wins regardless of depth.
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/root\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(tmp, "service")
	if err := os.Mkdir(service, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(service, ".git"), 0750); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(service, "pkg")
	if err := os.Mkdir(pkg, 0750); err != nil {
		t.Fatal(err)
	}

	got := util.FindProjectRoot(pkg)
	if want := canonical(t, service); got != want {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q (.git wins over parent go.mod)", pkg, got, want)
	}
}

func TestFindProjectRoot_ReturnsEmpty_WhenNoRootFound(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	// No .git, no go.mod — walk will terminate at filesystem root with no match

	got := util.FindProjectRoot(tmp)
	if got != "" {
		t.Fatalf("FindProjectRoot(%q) = %q, want empty", tmp, got)
	}
}

func TestFindProjectRoot_ReturnsStart_WhenStartIsRoot(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0750); err != nil {
		t.Fatal(err)
	}

	got := util.FindProjectRoot(tmp)
	if want := canonical(t, tmp); got != want {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", tmp, got, want)
	}
}

// TestFindProjectRoot_ResolvesSymlinks_WhenStartIsSymlinked guards sn-za8p:
// query-side commands resolve project root from os.Getwd(), which macOS
// returns already canonicalized (/private/var/... not /var/...). Index-side
// resolved an explicit `snipe index <path>` arg with plain filepath.Abs, so a
// symlinked start (e.g. a CI temp dir) left the returned root unresolved —
// the two sides then disagreed on file_path and every store lookup silently
// returned zero rows. FindProjectRoot must canonicalize regardless of which
// side is calling it.
func TestFindProjectRoot_ResolvesSymlinks_WhenStartIsSymlinked(t *testing.T) {
	t.Parallel()

	realDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(realDir, ".git"), 0750); err != nil {
		t.Fatal(err)
	}

	symParent := t.TempDir()
	symDir := filepath.Join(symParent, "symlinked-repo")
	if err := os.Symlink(realDir, symDir); err != nil {
		t.Fatal(err)
	}

	viaSymlink := util.FindProjectRoot(symDir)
	viaCanonical := util.FindProjectRoot(canonical(t, realDir))

	if viaSymlink != viaCanonical {
		t.Fatalf("FindProjectRoot(symlinked start) = %q, want %q (same as canonical start) — index and query sides would disagree on root", viaSymlink, viaCanonical)
	}
	if want := canonical(t, realDir); viaSymlink != want {
		t.Fatalf("FindProjectRoot(%q) = %q, want canonicalized %q", symDir, viaSymlink, want)
	}
}

// canonical resolves symlinks in p, failing the test if p doesn't exist. Used
// to match FindProjectRoot's canonicalized return value against expectations
// built from t.TempDir(), which on macOS is itself a symlink
// (/var/folders -> /private/var/folders).
func canonical(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}
