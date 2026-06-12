package query

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

const testModulePath = "github.com/example/myproject"

func setupDepsTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Create minimal schema needed for deps queries
	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT,
			kind TEXT,
			file_path TEXT,
			file_path_rel TEXT,
			pkg_path TEXT,
			line_start INTEGER,
			col_start INTEGER,
			line_end INTEGER,
			col_end INTEGER,
			signature TEXT,
			doc TEXT,
			receiver TEXT
		);
		CREATE TABLE imports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL,
			pkg_path TEXT NOT NULL,
			name TEXT,
			line INT NOT NULL,
			col INT NOT NULL,
			importer_pkg TEXT
		);
		CREATE TABLE meta (
			key TEXT PRIMARY KEY,
			value TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Insert a symbol so DetectModulePath works
	_, err = db.Exec(`INSERT INTO symbols (id, name, kind, pkg_path, file_path, file_path_rel, line_start, col_start, line_end, col_end)
		VALUES ('root1', 'Main', 'function', ?, '', '', 0, 0, 0, 0)`, testModulePath)
	if err != nil {
		t.Fatalf("insert symbol: %v", err)
	}

	return db
}

func insertImport(t *testing.T, db *sql.DB, filePath, pkgPath, importerPkg string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO imports (file_path, pkg_path, line, col, importer_pkg)
		VALUES (?, ?, 1, 1, ?)`, filePath, pkgPath, importerPkg)
	if err != nil {
		t.Fatalf("insert import: %v", err)
	}
}

func TestDetectModulePath(t *testing.T) {
	db := setupDepsTestDB(t)
	defer db.Close()

	got := DetectModulePath(db)
	if got != testModulePath {
		t.Errorf("DetectModulePath = %q, want %q", got, testModulePath)
	}
}

func TestDetectModulePath_MetaWins(t *testing.T) {
	db := setupDepsTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('module_path', 'github.com/meta/wins')`); err != nil {
		t.Fatalf("insert meta: %v", err)
	}

	if got := DetectModulePath(db); got != "github.com/meta/wins" {
		t.Errorf("DetectModulePath = %q, want meta value", got)
	}
}

func TestDetectModulePath_GoModFallback_NoRootPackage(t *testing.T) {
	// Repro for the loto failure: every package under internal/ or cmd/,
	// so the pkg_path heuristic finds nothing. go.mod at repo_root must win.
	db := setupDepsTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`UPDATE symbols SET pkg_path = ?`, testModulePath+"/internal/store"); err != nil {
		t.Fatalf("update symbols: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/example/gomod\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('repo_root', ?)`, root); err != nil {
		t.Fatalf("insert meta: %v", err)
	}

	if got := DetectModulePath(db); got != "github.com/example/gomod" {
		t.Errorf("DetectModulePath = %q, want go.mod module path", got)
	}
}

func TestDetectModulePath_HeuristicFallback_NoMetaNoGoMod(t *testing.T) {
	db := setupDepsTestDB(t)
	defer db.Close()

	// repo_root points somewhere with no go.mod; heuristic must still work.
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('repo_root', ?)`, t.TempDir()); err != nil {
		t.Fatalf("insert meta: %v", err)
	}

	if got := DetectModulePath(db); got != testModulePath {
		t.Errorf("DetectModulePath = %q, want heuristic value %q", got, testModulePath)
	}
}

func TestResolveFullPkgPath(t *testing.T) {
	db := setupDepsTestDB(t)
	defer db.Close()

	storePkg := testModulePath + "/internal/store"
	queryPkg := testModulePath + "/internal/query"
	insertImport(t, db, "/a/store/write.go", queryPkg, storePkg)

	// Full path returns unchanged
	got := ResolveFullPkgPath(db, storePkg, testModulePath)
	if got != storePkg {
		t.Errorf("full path: got %q, want %q", got, storePkg)
	}

	// Relative path gets module prefix
	got = ResolveFullPkgPath(db, "internal/store", testModulePath)
	if got != storePkg {
		t.Errorf("relative path: got %q, want %q", got, storePkg)
	}

	// Unknown path returns unchanged
	got = ResolveFullPkgPath(db, "nonexistent/pkg", testModulePath)
	if got != "nonexistent/pkg" {
		t.Errorf("unknown path: got %q, want %q", got, "nonexistent/pkg")
	}
}

func TestFindPackageDeps(t *testing.T) {
	db := setupDepsTestDB(t)
	defer db.Close()

	storePkg := testModulePath + "/internal/store"
	queryPkg := testModulePath + "/internal/query"
	cmdPkg := testModulePath + "/cmd"

	// store imports query (from 2 files)
	insertImport(t, db, "/a/store/write.go", queryPkg, storePkg)
	insertImport(t, db, "/a/store/read.go", queryPkg, storePkg)
	// cmd imports store
	insertImport(t, db, "/a/cmd/main.go", storePkg, cmdPkg)

	deps, err := FindPackageDeps(db, storePkg, testModulePath)
	if err != nil {
		t.Fatalf("FindPackageDeps: %v", err)
	}

	// store depends on query
	if len(deps.Dependencies) != 1 {
		t.Fatalf("dependencies: got %d, want 1", len(deps.Dependencies))
	}
	if deps.Dependencies[0].Package != "internal/query" {
		t.Errorf("dependency package = %q, want %q", deps.Dependencies[0].Package, "internal/query")
	}
	if deps.Dependencies[0].FileCount != 2 {
		t.Errorf("dependency file_count = %d, want 2", deps.Dependencies[0].FileCount)
	}

	// cmd depends on store
	if len(deps.Dependents) != 1 {
		t.Fatalf("dependents: got %d, want 1", len(deps.Dependents))
	}
	if deps.Dependents[0].Package != "cmd" {
		t.Errorf("dependent package = %q, want %q", deps.Dependents[0].Package, "cmd")
	}
}

func TestFindPackageDeps_FiltersExternal(t *testing.T) {
	db := setupDepsTestDB(t)
	defer db.Close()

	storePkg := testModulePath + "/internal/store"

	// store imports stdlib (should be filtered out)
	insertImport(t, db, "/a/store/db.go", "database/sql", storePkg)
	// store imports external (should be filtered out)
	insertImport(t, db, "/a/store/db.go", "github.com/other/lib", storePkg)

	deps, err := FindPackageDeps(db, storePkg, testModulePath)
	if err != nil {
		t.Fatalf("FindPackageDeps: %v", err)
	}

	if len(deps.Dependencies) != 0 {
		t.Errorf("expected 0 dependencies (external filtered), got %d", len(deps.Dependencies))
	}
}

func TestFindDepGraph(t *testing.T) {
	db := setupDepsTestDB(t)
	defer db.Close()

	storePkg := testModulePath + "/internal/store"
	queryPkg := testModulePath + "/internal/query"
	cmdPkg := testModulePath + "/cmd"

	// cmd -> store, store -> query
	insertImport(t, db, "/a/cmd/main.go", storePkg, cmdPkg)
	insertImport(t, db, "/a/store/write.go", queryPkg, storePkg)

	graph, err := FindDepGraph(db, testModulePath)
	if err != nil {
		t.Fatalf("FindDepGraph: %v", err)
	}

	if len(graph.Edges) != 2 {
		t.Fatalf("edges: got %d, want 2", len(graph.Edges))
	}

	sort.Strings(graph.Packages)
	wantPkgs := []string{"cmd", "internal/query", "internal/store"}
	sort.Strings(wantPkgs)
	if len(graph.Packages) != len(wantPkgs) {
		t.Fatalf("packages: got %v, want %v", graph.Packages, wantPkgs)
	}
	for i := range wantPkgs {
		if graph.Packages[i] != wantPkgs[i] {
			t.Errorf("packages[%d] = %q, want %q", i, graph.Packages[i], wantPkgs[i])
		}
	}

	if len(graph.Cycles) != 0 {
		t.Errorf("expected no cycles, got %v", graph.Cycles)
	}
}

func TestDetectCycles_NoCycles(t *testing.T) {
	edges := []DepGraphEdge{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
	}
	cycles := detectCycles(edges)
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %v", cycles)
	}
}

func TestDetectCycles_Simple(t *testing.T) {
	edges := []DepGraphEdge{
		{From: "a", To: "b"},
		{From: "b", To: "a"},
	}
	cycles := detectCycles(edges)
	if len(cycles) == 0 {
		t.Fatal("expected cycle, got none")
	}
	// Should contain both a and b
	cycle := cycles[0]
	if len(cycle) != 2 {
		t.Errorf("cycle length = %d, want 2; cycle = %v", len(cycle), cycle)
	}
}

func TestDetectCycles_Transitive(t *testing.T) {
	edges := []DepGraphEdge{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
		{From: "c", To: "a"},
	}
	cycles := detectCycles(edges)
	if len(cycles) == 0 {
		t.Fatal("expected cycle, got none")
	}
	cycle := cycles[0]
	if len(cycle) != 3 {
		t.Errorf("cycle length = %d, want 3; cycle = %v", len(cycle), cycle)
	}
}

func TestDetectCycles_MultipleRoots(t *testing.T) {
	// Two disconnected subgraphs: x->y->x (cycle) and a->b (no cycle).
	// The cycle in x->y should be correctly reconstructed regardless of DFS visit order.
	edges := []DepGraphEdge{
		{From: "a", To: "b"},
		{From: "x", To: "y"},
		{From: "y", To: "x"},
	}
	cycles := detectCycles(edges)
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d: %v", len(cycles), cycles)
	}
	cycle := cycles[0]
	if len(cycle) != 2 {
		t.Errorf("cycle length = %d, want 2; cycle = %v", len(cycle), cycle)
	}
	// Cycle should contain exactly x and y
	has := map[string]bool{cycle[0]: true, cycle[1]: true}
	if !has["x"] || !has["y"] {
		t.Errorf("cycle should contain x and y, got %v", cycle)
	}
}
