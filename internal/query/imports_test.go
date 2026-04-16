package query

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupImportsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE imports (
			file_path TEXT,
			pkg_path TEXT,
			name TEXT,
			line INTEGER,
			col INTEGER,
			importer_pkg TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return db
}

func TestIsBuildCachePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/Users/me/src/foo.go", false},
		{"/home/user/project/main.go", false},
		{"/Users/me/Library/Caches/go-build/50/50cc44abc123.go", true},
		{"/home/user/.cache/go-build/ab/abcdef.go", true},
	}
	for _, tc := range cases {
		got := isBuildCachePath(tc.path)
		if got != tc.want {
			t.Errorf("isBuildCachePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestFindImportersByDirectory_SkipsBuildCache(t *testing.T) {
	db := setupImportsDB(t)
	defer db.Close()

	insert := func(filePath, pkgPath string) {
		_, err := db.Exec(`INSERT INTO imports (file_path, pkg_path, line, col, importer_pkg) VALUES (?, ?, 1, 1, ?)`,
			filePath, pkgPath, "example.com/importer")
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	// Two legit importers and one build-cache leak.
	insert("/repo/src/a.go", "internal/graph")
	insert("/repo/src/b.go", "internal/graph")
	insert("/Users/u/Library/Caches/go-build/50/50cc44abc.go", "internal/graph")

	results, err := FindImportersByDirectory(db, "internal/graph", 50, 0)
	if err != nil {
		t.Fatalf("FindImportersByDirectory: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (build cache filtered), got %d", len(results))
	}
	for _, r := range results {
		if isBuildCachePath(r.FilePath) {
			t.Errorf("cache path leaked: %s", r.FilePath)
		}
	}
}
