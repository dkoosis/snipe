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
	if got != tmp {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", sub, got, tmp)
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
	if got != tmp {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", sub, got, tmp)
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
	if got != sub {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", sub, got, sub)
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
	if got != tmp {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q (git root)", pkg, got, tmp)
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
	if got != service {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q (.git wins over parent go.mod)", pkg, got, service)
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
	if got != tmp {
		t.Fatalf("FindProjectRoot(%q) = %q, want %q", tmp, got, tmp)
	}
}
