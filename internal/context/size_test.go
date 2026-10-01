package context

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

func TestCountFilesAndLines(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	db := s.DB()

	if _, err := db.Exec(`INSERT INTO files (path, mtime, hash, lines) VALUES
		('/r/a.go', 1, 'h', 120), ('/r/a_test.go', 1, 'h', 80), ('/r/b.go', 1, 'h', 7)`); err != nil {
		t.Fatal(err)
	}
	files, lines := countFilesAndLines(db)
	if files != 3 || lines != 207 {
		t.Errorf("countFilesAndLines = %d files, %d lines; want 3, 207", files, lines)
	}

	// A row with no count (index built before schema v22): files still
	// counts, lines is withheld rather than reported as a partial sum.
	if _, err := db.Exec(`INSERT INTO files (path, mtime, hash) VALUES ('/r/c.go', 1, 'h')`); err != nil {
		t.Fatal(err)
	}
	files, lines = countFilesAndLines(db)
	if files != 4 || lines != 0 {
		t.Errorf("countFilesAndLines with an uncounted file = %d files, %d lines; want 4, 0", files, lines)
	}
}
