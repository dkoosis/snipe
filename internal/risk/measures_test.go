package risk

import (
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

var storeSym = []changedSym{{id: "sym1", pkgPath: "github.com/x/internal/store"}}

func TestImporters_CountsDistinctOtherPackages(t *testing.T) {
	t.Parallel()
	s := seedStore(t, t.TempDir())
	if got := importers(s.DB(), storeSym); got != 2 {
		t.Fatalf("importers = %d, want 2 (cmd and internal/util; not itself)", got)
	}
	if got := importers(s.DB(), []changedSym{{pkgPath: "github.com/x/unknown"}}); got != 0 {
		t.Fatalf("importers of an unimported package = %d, want 0", got)
	}
}

func TestCommits_IsTheMostForAnyChangedFile(t *testing.T) {
	t.Parallel()
	s := seedStore(t, t.TempDir())
	if got := commits(s, []string{"cmd/cold.go", "internal/store/store.go", "new.go"}); got != 40 {
		t.Fatalf("commits = %d, want 40", got)
	}
}

func TestRoles_CountsChangedSymbolsPerRole(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	s := seedStore(t, repoRoot)
	byRole, _ := roles(s.DB(), repoRoot, storeSym)
	if byRole["persistence"] != 1 {
		t.Fatalf("roles = %v, want persistence: 1", byRole)
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
	if !v.Degraded || v.Note == "" {
		t.Fatalf("expected a degraded result with a note outside a git tree, got %+v", v)
	}
}
