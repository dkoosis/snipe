package query

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const testPkgStore = "github.com/example/myproject/internal/store"

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

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
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert symbols across multiple packages
	symbols := []struct {
		id, name, kind, pkgPath string
	}{
		{"a1", "Widget", "struct", "github.com/example/myproject"},
		{"a2", "Caller", "function", "github.com/example/myproject"},
		{"b1", "Lookup", "function", "github.com/example/myproject/internal/query"},
		{"b2", "SymbolRow", "struct", "github.com/example/myproject/internal/query"},
		{"c1", "Open", "function", testPkgStore},
	}
	for _, s := range symbols {
		_, err := db.Exec(`INSERT INTO symbols (id, name, kind, pkg_path, file_path, file_path_rel, line_start, col_start, line_end, col_end)
			VALUES (?, ?, ?, ?, '', '', 0, 0, 0, 0)`, s.id, s.name, s.kind, s.pkgPath)
		if err != nil {
			t.Fatalf("insert symbol: %v", err)
		}
	}

	return db
}

func TestResolvePkgPattern_Main(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got := ResolvePkgPattern(db, "main", "/tmp", "/tmp")
	if got != "github.com/example/myproject" {
		t.Errorf("main: got %q, want %q", got, "github.com/example/myproject")
	}
}

func TestResolvePkgPattern_Dot_AtRepoRoot(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got := ResolvePkgPattern(db, ".", "/tmp/myproject", "/tmp/myproject")
	if got != "github.com/example/myproject" {
		t.Errorf("dot at root: got %q, want %q", got, "github.com/example/myproject")
	}
}

func TestResolvePkgPattern_Dot_InSubdir(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got := ResolvePkgPattern(db, ".", "/tmp/myproject/internal/query", "/tmp/myproject")
	if got != "github.com/example/myproject/internal/query" {
		t.Errorf("dot in subdir: got %q, want %q", got, "github.com/example/myproject/internal/query")
	}
}

func TestResolvePkgPattern_ShortName_Resolves(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got := ResolvePkgPattern(db, "store", "/tmp", "/tmp")
	want := testPkgStore
	if got != want {
		t.Errorf("short name: got %q, want %q", got, want)
	}
}

func TestResolvePkgPattern_ShortName_NoMatch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got := ResolvePkgPattern(db, "nonexistent", "/tmp", "/tmp")
	if got != "nonexistent" {
		t.Errorf("no match passthrough: got %q, want %q", got, "nonexistent")
	}
}

func TestResolvePkgPattern_Passthrough_RelativePath(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got := ResolvePkgPattern(db, "internal/query", "/tmp", "/tmp")
	if got != "internal/query" {
		t.Errorf("passthrough: got %q, want %q", got, "internal/query")
	}
}

func TestResolvePkgPattern_Passthrough_FullModulePath(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	got := ResolvePkgPattern(db, testPkgStore, "/tmp", "/tmp")
	if got != testPkgStore {
		t.Errorf("passthrough: got %q, want %q", got, testPkgStore)
	}
}

func TestResolvePkgPattern_Dot_WithRealPaths(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Use real temp dirs to exercise filepath.Rel with real OS paths
	root := t.TempDir()
	subdir := filepath.Join(root, "internal", "store")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := ResolvePkgPattern(db, ".", subdir, root)
	if got != testPkgStore {
		t.Errorf("dot with real paths: got %q, want %q", got, testPkgStore)
	}
}
