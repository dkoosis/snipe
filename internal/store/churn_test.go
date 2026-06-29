package store

import (
	"path/filepath"
	"testing"
)

func churnTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestWriteAndReadFileChurn(t *testing.T) {
	s := churnTestStore(t)

	rows := []FileChurn{
		{Path: "a.go", Commits: 10, Authors: 3, FirstSeen: "2026-01-01", LastChanged: "2026-06-01", Score: 4.5},
		{Path: "b.go", Commits: 25, Authors: 1, FirstSeen: "2025-01-01", LastChanged: "2026-05-01", Score: 9.0},
		{Path: "c.go", Commits: 25, Authors: 2, FirstSeen: "2025-06-01", LastChanged: "2026-04-01", Score: 7.0},
	}
	if err := s.WriteFileChurn(rows); err != nil {
		t.Fatalf("WriteFileChurn: %v", err)
	}

	got, err := s.ReadFileChurnTopN(0)
	if err != nil {
		t.Fatalf("ReadFileChurnTopN: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	// Ordered by commits desc, then path asc: b(25), c(25), a(10).
	if got[0].Path != "b.go" || got[1].Path != "c.go" || got[2].Path != "a.go" {
		t.Errorf("order = %s,%s,%s; want b.go,c.go,a.go", got[0].Path, got[1].Path, got[2].Path)
	}
	if got[0].Authors != 1 || got[0].Score != 9.0 {
		t.Errorf("b.go fields not round-tripped: %+v", got[0])
	}

	// Top-N truncates after ranking.
	top, err := s.ReadFileChurnTopN(2)
	if err != nil {
		t.Fatalf("ReadFileChurnTopN(2): %v", err)
	}
	if len(top) != 2 || top[0].Path != "b.go" || top[1].Path != "c.go" {
		t.Errorf("top-2 = %+v; want b.go,c.go", top)
	}
}

func TestWriteFileChurnReplaces(t *testing.T) {
	s := churnTestStore(t)
	if err := s.WriteFileChurn([]FileChurn{{Path: "old.go", Commits: 5}}); err != nil {
		t.Fatal(err)
	}
	// Second write replaces the whole table.
	if err := s.WriteFileChurn([]FileChurn{{Path: "new.go", Commits: 1}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFileChurnTopN(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "new.go" {
		t.Errorf("replace failed, got %+v", got)
	}

	// Empty write clears the table (non-git reindex path).
	if err := s.WriteFileChurn(nil); err != nil {
		t.Fatal(err)
	}
	got, err = s.ReadFileChurnTopN(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty write should clear table, got %+v", got)
	}
}
