package risk

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

// seedStore opens a temp index with one store package imported by two others,
// one file with commit history, and one persistence-role symbol, so the
// store-backed measures have something to read.
func seedStore(t *testing.T, repoRoot string) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// internal/store is imported by cmd (twice, from two files) and internal/util,
	// and imports itself from a test file (which must not count).
	for _, imp := range [][2]string{
		{"cmd/a.go", "github.com/x/cmd"},
		{"cmd/b.go", "github.com/x/cmd"},
		{"internal/util/u.go", "github.com/x/internal/util"},
		{"internal/store/s_test.go", "github.com/x/internal/store"},
	} {
		if _, err := s.DB().Exec(`
			INSERT INTO imports (file_path, pkg_path, line, col, importer_pkg)
			VALUES (?, 'github.com/x/internal/store', 3, 2, ?)
		`, imp[0], imp[1]); err != nil {
			t.Fatalf("insert import: %v", err)
		}
	}
	if err := s.WriteFileChurn([]store.FileChurn{
		{Path: "internal/store/store.go", Commits: 40, Authors: 3, Score: 9.9},
		{Path: "cmd/cold.go", Commits: 2, Authors: 1, Score: 0.1},
	}); err != nil {
		t.Fatalf("write churn: %v", err)
	}
	// A persistence-role symbol: a func in a *store* package (inferRole tags store
	// packages persistence). file_path is absolute (repoRoot-joined).
	abs := filepath.Join(repoRoot, "internal/store/store.go")
	_, err = s.DB().Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path,
			line_start, col_start, line_end, col_end)
		VALUES ('sym1', 'WriteIndex', 'func', ?, 'internal/store/store.go',
			'github.com/x/internal/store', 10, 1, 30, 1)
	`, abs)
	if err != nil {
		t.Fatalf("insert symbol: %v", err)
	}
	return s
}

var storeSym = []changedSym{{id: "sym1", name: "WriteIndex", kind: "func", pkgPath: "github.com/x/internal/store"}}

func TestImporters_CountsDistinctOtherPackages(t *testing.T) {
	t.Parallel()
	s := seedStore(t, t.TempDir())
	if got := reach(s, storeSym).Importers; got != 2 {
		t.Fatalf("importers = %d, want 2 (cmd and internal/util; not itself)", got)
	}
	if got := reach(s, []changedSym{{pkgPath: "github.com/x/unknown"}}).Importers; got != 0 {
		t.Fatalf("importers of an unimported package = %d, want 0", got)
	}
}

func TestHistory_IsTheMostForAnyChangedFile(t *testing.T) {
	t.Parallel()
	s := seedStore(t, t.TempDir())
	h := history(s, []string{"cmd/cold.go", "internal/store/store.go", "new.go"})
	if h == nil || h.Commits != 40 || h.Authors != 3 || h.Churn != 9.9 {
		t.Fatalf("history = %+v, want commits 40, authors 3, churn 9.9", h)
	}
}

func TestHistory_NilWithoutChurn(t *testing.T) {
	t.Parallel()
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if h := history(s, []string{"a.go"}); h != nil {
		t.Fatalf("history with no churn = %+v, want nil (missing, not zero)", h)
	}
}

func TestRoles_CountsChangedSymbolsPerRole(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	s := seedStore(t, repoRoot)
	k := kind(s.DB(), repoRoot, storeSym)
	if k.Roles["persistence"] != 1 || k.Exported != 1 {
		t.Fatalf("kind = %+v, want persistence: 1, exported 1", k)
	}
}

func TestChangedSymbols_ExcludesTestFiles(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	s := seedStore(t, repoRoot)
	// Seed a symbol in a _test.go file at the same overlapping range.
	abs := filepath.Join(repoRoot, "internal/store/store_test.go")
	if _, err := s.DB().Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path,
			line_start, col_start, line_end, col_end)
		VALUES ('symT', 'TestWriteIndex', 'func', ?, 'internal/store/store_test.go',
			'github.com/x/internal/store', 10, 1, 30, 1)
	`, abs); err != nil {
		t.Fatalf("insert test symbol: %v", err)
	}
	changes := []FileChange{{Path: "internal/store/store_test.go", LineRanges: [][2]int{{10, 30}}}}
	if got := changedSymbols(s.DB(), repoRoot, changes); len(got) != 0 {
		t.Fatalf("expected _test.go symbols excluded, got %+v", got)
	}
}

func TestAssess_DegradesWhenNotAGitWorkTree(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir() // empty dir, not a git repo
	s := seedStore(t, repoRoot)
	v := Assess(s, repoRoot, "HEAD~1", "HEAD")
	if !v.Degraded || v.Note == "" || v.Score != nil {
		t.Fatalf("expected a degraded result with a note and no score outside a git tree, got %+v", v)
	}
}

func TestComplexity_MeasuresOnlyTheChangedFunctions(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	src := `package p

func simple() {}

func branchy(a, b int) int {
	if a > 0 && b > 0 {
		for i := 0; i < a; i++ {
			if i == b {
				return i
			}
		}
	}
	return 0
}
`
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "core.hooksPath", "/dev/null"},
	} {
		gitRun(t, repo, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "x")

	// Only branchy (lines 5-14) is touched.
	cx := complexity(repo, "HEAD", []FileChange{{Path: "p.go", LineRanges: [][2]int{{7, 7}}}})
	got := cx["p.go"]
	if len(got) != 1 || got[0].name != "branchy" || got[0].cyclo != 5 || got[0].cognitive == 0 {
		t.Fatalf("complexity = %+v, want branchy alone with cyclo 5", got)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
