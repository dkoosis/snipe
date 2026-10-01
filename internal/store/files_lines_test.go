package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

func TestWriteFiles_StoresLineCounts(t *testing.T) {
	s := churnTestStore(t)
	if err := s.WriteFiles([]index.FileInfo{
		{Path: "/r/a.go", Mtime: 1, Hash: "h1", Lines: 120},
		{Path: "/r/b.go", Mtime: 1, Hash: "h2", Lines: 7},
	}); err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	var sum int
	if err := s.db.QueryRow(`SELECT SUM(lines) FROM files`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if sum != 127 {
		t.Errorf("SUM(lines) = %d, want 127", sum)
	}
}

// An index written at schema v21 gains files.lines on open, NULL for its
// existing rows until the next index run rewrites them.
func TestMigration22_AddsFilesLines_When_OpeningAV21Index(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE files DROP COLUMN lines`,
		`INSERT INTO files (path, mtime, hash) VALUES ('/r/a.go', 1, 'h')`,
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', '21')`,
		`DELETE FROM migrations WHERE version = 22`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_ = s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	var lines sql.NullInt64
	if err := s.db.QueryRow(`SELECT lines FROM files WHERE path = '/r/a.go'`).Scan(&lines); err != nil {
		t.Fatalf("files.lines after migration: %v", err)
	}
	if lines.Valid {
		t.Errorf("lines = %d for a pre-v22 row, want NULL", lines.Int64)
	}
}
