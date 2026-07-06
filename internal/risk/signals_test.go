package risk

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

// seedStore opens a temp index and inserts one central package, one hot file, and
// one persistence-role symbol so the store-backed gatherers have something to read.
func seedStore(t *testing.T, repoRoot string) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// PageRank: internal/store is #1 of two packages.
	if err := s.WriteGraphMetrics("imports", "pagerank", map[string]float64{
		"github.com/x/internal/store": 0.9,
		"github.com/x/internal/util":  0.1,
	}); err != nil {
		t.Fatalf("write metrics: %v", err)
	}
	// Churn: store.go is a hotspot.
	if err := s.WriteFileChurn([]store.FileChurn{
		{Path: "internal/store/store.go", Commits: 40, Authors: 3, Score: 9.9},
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

func TestCentralSignal_FiresOnTopRankedPackage(t *testing.T) {
	t.Parallel()
	s := seedStore(t, t.TempDir())
	got := centralSignal(s, []changedSym{{id: "sym1", pkgPath: "github.com/x/internal/store"}})
	if len(got) != 1 || got[0].Signal != sigCentral || got[0].Weight != weightStrong {
		t.Fatalf("central signal = %+v, want one strong central", got)
	}
}

func TestCentralSignal_SilentOnUnrankedPackage(t *testing.T) {
	t.Parallel()
	s := seedStore(t, t.TempDir())
	if got := centralSignal(s, []changedSym{{pkgPath: "github.com/x/unknown"}}); got != nil {
		t.Fatalf("expected no central signal, got %+v", got)
	}
}

func TestChurnSignal_FiresOnHotFile(t *testing.T) {
	t.Parallel()
	s := seedStore(t, t.TempDir())
	got := churnSignal(s, []string{"internal/store/store.go", "cmd/cold.go"})
	if len(got) != 1 || got[0].Signal != sigChurn {
		t.Fatalf("churn signal = %+v, want one churn-hotspot", got)
	}
}

func TestRoleSignals_FiresPersistenceOnStorePackageSymbol(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	s := seedStore(t, repoRoot)
	got := roleSignals(s.DB(), repoRoot, []changedSym{{id: "sym1", pkgPath: "github.com/x/internal/store"}})
	var found bool
	for _, sig := range got {
		if sig.Signal == sigRolePrefix+"persistence" && sig.Weight == weightStrong {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected role:persistence signal, got %+v", got)
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
	if !v.Degraded || v.Verdict != VerdictLow {
		t.Fatalf("expected degraded low verdict outside a git tree, got %+v", v)
	}
}
