package store

import (
	"testing"
)

func healthTestStore(t *testing.T) *Store {
	t.Helper()
	s := churnTestStore(t)
	_, err := s.db.Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, line_start, col_start, line_end, col_end) VALUES
			('f1', 'Run',     'func',   '/r/a.go',      'a.go',      1, 1, 2, 1),
			('m1', 'Close',   'method', '/r/a.go',      'a.go',      3, 1, 4, 1),
			('t1', 'Config',  'type',   '/r/a.go',      'a.go',      5, 1, 6, 1),
			('f2', 'Load',    'func',   '/r/b.go',      'b.go',      1, 1, 2, 1),
			('f3', 'TestRun', 'func',   '/r/a_test.go', 'a_test.go', 1, 1, 2, 1);
		INSERT INTO refs (id, symbol_id, file_path, line, col) VALUES ('r1', 'f1', '/r/b.go', 1, 1);`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return s
}

func TestIntegrityCheck_ReportsOK_When_DatabaseIsHealthy(t *testing.T) {
	s := healthTestStore(t)
	got, err := s.IntegrityCheck()
	if err != nil {
		t.Fatalf("IntegrityCheck: %v", err)
	}
	if got != "ok" {
		t.Errorf("IntegrityCheck = %q, want %q", got, "ok")
	}
}

func TestOrphanedRefCount(t *testing.T) {
	s := healthTestStore(t)

	got, err := s.OrphanedRefCount()
	if err != nil {
		t.Fatalf("OrphanedRefCount: %v", err)
	}
	if got != 0 {
		t.Errorf("OrphanedRefCount on clean index = %d, want 0", got)
	}

	// Foreign keys block an orphan in normal use; the check exists for indexes
	// written with them off (incremental writes toggle them).
	if _, err := s.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO refs (id, symbol_id, file_path, line, col) VALUES ('r2', 'gone', '/r/b.go', 2, 1)`); err != nil {
		t.Fatal(err)
	}
	got, err = s.OrphanedRefCount()
	if err != nil {
		t.Fatalf("OrphanedRefCount: %v", err)
	}
	if got != 1 {
		t.Errorf("OrphanedRefCount with one orphan = %d, want 1", got)
	}
}

func TestFileFuncCounts_CountsFuncsAndMethods_When_FileIsNotATest(t *testing.T) {
	s := healthTestStore(t)
	got, err := s.FileFuncCounts()
	if err != nil {
		t.Fatalf("FileFuncCounts: %v", err)
	}
	want := map[string]int{"a.go": 2, "b.go": 1}
	if len(got) != len(want) {
		t.Fatalf("FileFuncCounts = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("FileFuncCounts[%q] = %d, want %d", k, got[k], v)
		}
	}
}
