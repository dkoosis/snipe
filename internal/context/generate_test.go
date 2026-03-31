package context

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// setupGenerateDB creates an in-memory DB with symbols and refs tables.
func setupGenerateDB(t *testing.T) *sql.DB {
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
			line_start INTEGER,
			doc TEXT
		);
		CREATE TABLE refs (
			id TEXT PRIMARY KEY,
			symbol_id TEXT,
			file_path TEXT,
			line INTEGER,
			col INTEGER
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return db
}

func TestInferPurpose(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"cmd", "Command-line interface"},
		{"internal", "Internal implementation packages"},
		{"store", "Data storage and persistence"},
		{"query", "Query execution"},
		{"unknown", "Application logic"},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			got := inferPurpose(tt.dir)
			if got != tt.want {
				t.Errorf("inferPurpose(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func TestCategorizeByConcern(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"cmd/root.go", "cli"},
		{"internal/store/store.go", "storage"},
		{"internal/query/lookup.go", "query"},
		{"internal/index/refs.go", "indexing"},
		{"internal/output/types.go", "output"},
		{"test/bench/bench_test.go", "testing"},
		{"main.go", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			got := categorizeByConcern(tt.relPath)
			if got != tt.want {
				t.Errorf("categorizeByConcern(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestInferPackagePurpose_SpecificPackages(t *testing.T) {
	tests := []struct {
		pkg  string
		want string
	}{
		{"internal/vector", "Vector math for embedding similarity"},
		{"bench", "Benchmarks and baseline capture"},
		{"test/blackbox", "Integration tests (blackbox)"},
		{"test/bench", "Benchmarks and baseline capture"},
		{"internal/util", "Shared utility functions (project root, caching)"},
	}
	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			got := inferPackagePurpose(tt.pkg)
			if got != tt.want {
				t.Errorf("inferPackagePurpose(%q) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestDescribeFile(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"cmd/root.go", "CLI root command"},
		{"internal/store/store.go", "Database operations"},
		{"internal/store/schema.go", "Database schema"},
		{"internal/config/config.go", "Configuration handling"},
		{"internal/unknown/unknown.go", "Implementation"},
	}

	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			got := describeFile(tt.relPath)
			if got != tt.want {
				t.Errorf("describeFile(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestInferProjectPurpose_Fallback(t *testing.T) {
	// With no DB, should return empty
	purpose := inferProjectPurpose(nil, "/nonexistent", "myproject")
	if purpose != "" {
		t.Errorf("expected empty for nil DB, got %q", purpose)
	}
}

func TestInferProjectPurpose_FromReadme(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(
		"# myproject\n\nA fast CLI tool for indexing Go code.\n\nMore stuff here.\n",
	), 0644)

	purpose := inferProjectPurpose(nil, dir, "myproject")
	if purpose == "" {
		t.Error("expected purpose from README, got empty")
	}
	if purpose != "A fast CLI tool for indexing Go code." {
		t.Errorf("purpose = %q, want %q", purpose, "A fast CLI tool for indexing Go code.")
	}
}

func TestGetKeySymbolsByRefCount_IncludesPurpose(t *testing.T) {
	db := setupGenerateDB(t)
	defer db.Close()

	_, err := db.Exec(`INSERT INTO symbols (id, name, kind, file_path, line_start, doc)
		VALUES ('s1', 'Store', 'type', '/repo/store.go', 10, 'Store manages SQLite persistence.')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO refs (id, symbol_id, file_path, line, col)
		VALUES ('r1', 's1', '/repo/main.go', 5, 1)`)
	if err != nil {
		t.Fatal(err)
	}

	symbols := getKeySymbolsByRefCount(db, "/repo", 10)
	if len(symbols) == 0 {
		t.Fatal("expected at least one symbol")
	}
	if symbols[0].Purpose == "" {
		t.Error("expected purpose from doc comment, got empty")
	}
	if symbols[0].Purpose != "Store manages SQLite persistence." {
		t.Errorf("purpose = %q, want %q", symbols[0].Purpose, "Store manages SQLite persistence.")
	}
}
